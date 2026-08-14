// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/idempotency"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/requestmeta"
	sqlpostgres "mindclade.internal/libs/go/storage/sql/postgres"
	"mindclade.internal/libs/go/storage/sql/transaction"
)

type databaseExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Store is a PostgreSQL-backed idempotency store. The same Store may be shared
// concurrently by all handlers in one service process.
type Store struct {
	db        *sql.DB
	clock     clock.Clock
	generator *identifiers.Generator
	table     string
}

var _ idempotency.Store = (*Store)(nil)

func (store *Store) Acquire(ctx context.Context, request idempotency.AcquireRequest) (idempotency.Acquisition, error) {
	if err := store.validateContext(ctx); err != nil {
		return idempotency.Acquisition{}, err
	}
	request = request.Normalized()
	if err := request.Validate(); err != nil {
		return idempotency.Acquisition{}, err
	}
	if tx, ok := transaction.FromContext(ctx); ok {
		return store.acquire(ctx, tx, request)
	}
	return transaction.Run(ctx, store.db, transaction.Options{}, func(txCtx context.Context, tx *sql.Tx) (idempotency.Acquisition, error) {
		return store.acquire(txCtx, tx, request)
	})
}

func (store *Store) acquire(ctx context.Context, executor databaseExecutor, request idempotency.AcquireRequest) (idempotency.Acquisition, error) {
	now := store.clock.Now().Round(0).UTC()
	recordID, err := store.generator.IDAt(idempotency.RecordIDKind, now)
	if err != nil {
		return idempotency.Acquisition{}, internal(ctx, err, "idempotency.postgres.Acquire", "id_generation_failed")
	}
	token, err := store.generator.UUIDv4()
	if err != nil {
		return idempotency.Acquisition{}, internal(ctx, err, "idempotency.postgres.Acquire", "lease_token_generation_failed")
	}
	leaseExpiresAt := now.Add(request.LeaseDuration)
	expiresAt := now.Add(request.TTL)

	insertQuery := fmt.Sprintf(`INSERT INTO %s (
identity_digest, scope, idempotency_key, record_id, fingerprint, state,
result, request_id, created_at, updated_at, expires_at,
lease_token, lease_expires_at, version
) VALUES ($1,$2,$3,$4,$5,$6,NULL,$7,$8,$8,$9,$10,$11,1)
ON CONFLICT (identity_digest) DO NOTHING`, store.table)
	result, err := executor.ExecContext(ctx, insertQuery,
		request.Identity.Digest().String(), request.Identity.Scope.String(), request.Identity.Key.String(),
		recordID.String(), request.Fingerprint.String(), string(idempotency.StateInProgress),
		request.RequestID.String(), now, expiresAt, token.String(), leaseExpiresAt,
	)
	if err != nil {
		return idempotency.Acquisition{}, provider(ctx, err, "idempotency.postgres.Acquire.insert")
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr == nil && affected == 1 {
		record, recordErr := idempotency.NewRecord(idempotency.RecordData{
			ID: recordID, Identity: request.Identity, Fingerprint: request.Fingerprint,
			State: idempotency.StateInProgress, RequestID: request.RequestID,
			CreatedAt: now, UpdatedAt: now, ExpiresAt: expiresAt,
			LeaseExpiresAt: leaseExpiresAt, Version: 1,
		})
		if recordErr != nil {
			return idempotency.Acquisition{}, internal(ctx, recordErr, "idempotency.postgres.Acquire", "invalid_inserted_record")
		}
		lease := idempotency.Lease{RecordID: recordID, Identity: request.Identity, Fingerprint: request.Fingerprint, Token: token, ExpiresAt: leaseExpiresAt, Version: 1}
		return idempotency.Acquisition{Disposition: idempotency.DispositionAcquired, Record: record, Lease: lease}, nil
	}

	current, currentToken, err := store.selectRecord(ctx, executor, request.Identity.Digest().String(), true)
	if err != nil {
		return idempotency.Acquisition{}, err
	}
	if current.Expired(now) {
		return store.replaceExpired(ctx, executor, request, current, recordID, token, now, expiresAt, leaseExpiresAt)
	}
	if !current.Fingerprint().Equal(request.Fingerprint) {
		return idempotency.Acquisition{Disposition: idempotency.DispositionConflict, Record: current}, nil
	}
	if current.State() == idempotency.StateCompleted {
		return idempotency.Acquisition{Disposition: idempotency.DispositionReplay, Record: current}, nil
	}
	if !current.LeaseExpired(now) {
		return idempotency.Acquisition{Disposition: idempotency.DispositionInProgress, Record: current}, nil
	}
	_ = currentToken
	return store.reclaim(ctx, executor, current, request.RequestID, token, now, request.LeaseDuration)
}

func (store *Store) replaceExpired(ctx context.Context, executor databaseExecutor, request idempotency.AcquireRequest, current idempotency.Record, recordID identifiers.ID, token identifiers.UUID, now, expiresAt, leaseExpiresAt time.Time) (idempotency.Acquisition, error) {
	query := fmt.Sprintf(`UPDATE %s SET
scope=$2, idempotency_key=$3, record_id=$4, fingerprint=$5, state=$6,
result=NULL, request_id=$7, created_at=$8, updated_at=$8, expires_at=$9,
lease_token=$10, lease_expires_at=$11, version=version+1
WHERE identity_digest=$1 AND version=$12`, store.table)
	result, err := executor.ExecContext(ctx, query,
		request.Identity.Digest().String(), request.Identity.Scope.String(), request.Identity.Key.String(),
		recordID.String(), request.Fingerprint.String(), string(idempotency.StateInProgress), request.RequestID.String(),
		now, expiresAt, token.String(), leaseExpiresAt, current.Version(),
	)
	if err != nil {
		return idempotency.Acquisition{}, provider(ctx, err, "idempotency.postgres.Acquire.replace")
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return idempotency.Acquisition{}, leaseLost(ctx, current.ID(), "idempotency.postgres.Acquire.replace")
	}
	version := current.Version() + 1
	record, err := idempotency.NewRecord(idempotency.RecordData{
		ID: recordID, Identity: request.Identity, Fingerprint: request.Fingerprint,
		State: idempotency.StateInProgress, RequestID: request.RequestID,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: expiresAt, LeaseExpiresAt: leaseExpiresAt, Version: version,
	})
	if err != nil {
		return idempotency.Acquisition{}, internal(ctx, err, "idempotency.postgres.Acquire.replace", "invalid_replaced_record")
	}
	lease := idempotency.Lease{RecordID: recordID, Identity: request.Identity, Fingerprint: request.Fingerprint, Token: token, ExpiresAt: leaseExpiresAt, Version: version}
	return idempotency.Acquisition{Disposition: idempotency.DispositionAcquired, Record: record, Lease: lease}, nil
}

func (store *Store) reclaim(ctx context.Context, executor databaseExecutor, current idempotency.Record, requestID requestmeta.RequestID, token identifiers.UUID, now time.Time, duration time.Duration) (idempotency.Acquisition, error) {
	leaseExpiresAt := now.Add(duration)
	if !leaseExpiresAt.Before(current.ExpiresAt()) {
		leaseExpiresAt = current.ExpiresAt()
	}
	query := fmt.Sprintf(`UPDATE %s SET request_id=$2, updated_at=$3, lease_token=$4,
lease_expires_at=$5, version=version+1
WHERE identity_digest=$1 AND version=$6 AND state=$7 AND lease_expires_at <= $3`, store.table)
	result, err := executor.ExecContext(ctx, query, current.Identity().Digest().String(), requestID.String(), now, token.String(), leaseExpiresAt, current.Version(), string(idempotency.StateInProgress))
	if err != nil {
		return idempotency.Acquisition{}, provider(ctx, err, "idempotency.postgres.Acquire.reclaim")
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return idempotency.Acquisition{}, leaseLost(ctx, current.ID(), "idempotency.postgres.Acquire.reclaim")
	}
	data := current.Data()
	data.RequestID = requestID
	data.UpdatedAt = now
	data.LeaseExpiresAt = leaseExpiresAt
	data.Version++
	record, err := idempotency.NewRecord(data)
	if err != nil {
		return idempotency.Acquisition{}, internal(ctx, err, "idempotency.postgres.Acquire.reclaim", "invalid_reclaimed_record")
	}
	lease := idempotency.Lease{RecordID: record.ID(), Identity: record.Identity(), Fingerprint: record.Fingerprint(), Token: token, ExpiresAt: leaseExpiresAt, Version: record.Version()}
	return idempotency.Acquisition{Disposition: idempotency.DispositionAcquired, Record: record, Lease: lease}, nil
}

func (store *Store) Complete(ctx context.Context, request idempotency.CompleteRequest) (idempotency.Record, error) {
	if err := store.validateContext(ctx); err != nil {
		return idempotency.Record{}, err
	}
	if err := request.Validate(); err != nil {
		return idempotency.Record{}, err
	}
	resultJSON, err := json.Marshal(request.Result)
	if err != nil {
		return idempotency.Record{}, internal(ctx, err, "idempotency.postgres.Complete", "result_encoding_failed")
	}
	executor := store.executor(ctx)
	now := store.clock.Now().Round(0).UTC()
	query := fmt.Sprintf(`UPDATE %s SET state=$1, result=$2, updated_at=$3,
lease_token=NULL, lease_expires_at=NULL, version=version+1
WHERE identity_digest=$4 AND record_id=$5 AND fingerprint=$6 AND lease_token=$7
AND version=$8 AND state=$9 AND lease_expires_at > $3
RETURNING scope,idempotency_key,record_id,fingerprint,state,result,request_id,
created_at,updated_at,expires_at,lease_token,lease_expires_at,version`, store.table)
	row := executor.QueryRowContext(ctx, query,
		string(idempotency.StateCompleted), resultJSON, now, request.Lease.Identity.Digest().String(),
		request.Lease.RecordID.String(), request.Lease.Fingerprint.String(), request.Lease.Token.String(),
		request.Lease.Version, string(idempotency.StateInProgress),
	)
	record, _, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return idempotency.Record{}, leaseLost(ctx, request.Lease.RecordID, "idempotency.postgres.Complete")
	}
	if err != nil {
		return idempotency.Record{}, provider(ctx, err, "idempotency.postgres.Complete")
	}
	return record, nil
}

func (store *Store) Release(ctx context.Context, request idempotency.ReleaseRequest) error {
	if err := store.validateContext(ctx); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	now := store.clock.Now().Round(0).UTC()
	query := fmt.Sprintf(`DELETE FROM %s WHERE identity_digest=$1 AND record_id=$2
AND fingerprint=$3 AND lease_token=$4 AND version=$5 AND state=$6 AND lease_expires_at > $7`, store.table)
	result, err := store.executor(ctx).ExecContext(ctx, query,
		request.Lease.Identity.Digest().String(), request.Lease.RecordID.String(), request.Lease.Fingerprint.String(),
		request.Lease.Token.String(), request.Lease.Version, string(idempotency.StateInProgress), now,
	)
	if err != nil {
		return provider(ctx, err, "idempotency.postgres.Release")
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return leaseLost(ctx, request.Lease.RecordID, "idempotency.postgres.Release")
	}
	return nil
}

func (store *Store) Renew(ctx context.Context, request idempotency.RenewRequest) (idempotency.Lease, error) {
	if err := store.validateContext(ctx); err != nil {
		return idempotency.Lease{}, err
	}
	if err := request.Validate(); err != nil {
		return idempotency.Lease{}, err
	}
	now := store.clock.Now().Round(0).UTC()
	requested := now.Add(request.ExtendBy)
	query := fmt.Sprintf(`UPDATE %s SET updated_at=$1, lease_expires_at=LEAST(expires_at,$2), version=version+1
WHERE identity_digest=$3 AND record_id=$4 AND fingerprint=$5 AND lease_token=$6
AND version=$7 AND state=$8 AND lease_expires_at > $1 AND expires_at > $1
RETURNING scope,idempotency_key,record_id,fingerprint,state,result,request_id,
created_at,updated_at,expires_at,lease_token,lease_expires_at,version`, store.table)
	row := store.executor(ctx).QueryRowContext(ctx, query,
		now, requested, request.Lease.Identity.Digest().String(), request.Lease.RecordID.String(),
		request.Lease.Fingerprint.String(), request.Lease.Token.String(), request.Lease.Version, string(idempotency.StateInProgress),
	)
	record, token, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return idempotency.Lease{}, leaseLost(ctx, request.Lease.RecordID, "idempotency.postgres.Renew")
	}
	if err != nil {
		return idempotency.Lease{}, provider(ctx, err, "idempotency.postgres.Renew")
	}
	return idempotency.Lease{RecordID: record.ID(), Identity: record.Identity(), Fingerprint: record.Fingerprint(), Token: token, ExpiresAt: record.LeaseExpiresAt(), Version: record.Version()}, nil
}

func (store *Store) Lookup(ctx context.Context, identity idempotency.Identity) (idempotency.Record, error) {
	if err := store.validateContext(ctx); err != nil {
		return idempotency.Record{}, err
	}
	if err := identity.Validate(); err != nil {
		return idempotency.Record{}, err
	}
	record, _, err := store.selectRecord(ctx, store.executor(ctx), identity.Digest().String(), false)
	if err != nil {
		return idempotency.Record{}, err
	}
	if record.Expired(store.clock.Now().Round(0).UTC()) {
		_, _ = store.executor(ctx).ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE identity_digest=$1 AND version=$2`, store.table), identity.Digest().String(), record.Version())
		return idempotency.Record{}, notFound(ctx, identity)
	}
	return record, nil
}

func (store *Store) selectRecord(ctx context.Context, executor databaseExecutor, digest string, forUpdate bool) (idempotency.Record, identifiers.UUID, error) {
	query := fmt.Sprintf(`SELECT scope,idempotency_key,record_id,fingerprint,state,result,request_id,
created_at,updated_at,expires_at,lease_token,lease_expires_at,version FROM %s WHERE identity_digest=$1`, store.table)
	if forUpdate {
		query += " FOR UPDATE"
	}
	record, token, err := scanRecord(executor.QueryRowContext(ctx, query, digest))
	if errors.Is(err, sql.ErrNoRows) {
		return idempotency.Record{}, identifiers.UUID{}, faults.Wrap(ErrRecordMissing, faults.CodeNotFound, "idempotency record was not found", faults.WithReason(idempotency.ReasonNotFound), faults.WithOperation("idempotency.postgres.Lookup"), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err != nil {
		return idempotency.Record{}, identifiers.UUID{}, provider(ctx, err, "idempotency.postgres.select")
	}
	return record, token, nil
}

func scanRecord(row *sql.Row) (idempotency.Record, identifiers.UUID, error) {
	var scopeText, keyText, recordIDText, fingerprintText, stateText string
	var resultJSON []byte
	var requestIDText, leaseTokenText sql.NullString
	var createdAt, updatedAt, expiresAt time.Time
	var leaseExpiresAt sql.NullTime
	var version uint64
	if err := row.Scan(&scopeText, &keyText, &recordIDText, &fingerprintText, &stateText, &resultJSON, &requestIDText, &createdAt, &updatedAt, &expiresAt, &leaseTokenText, &leaseExpiresAt, &version); err != nil {
		return idempotency.Record{}, identifiers.UUID{}, err
	}
	scope, err := idempotency.ParseScope(scopeText)
	if err != nil {
		return idempotency.Record{}, identifiers.UUID{}, err
	}
	key, err := idempotency.ParseKey(keyText)
	if err != nil {
		return idempotency.Record{}, identifiers.UUID{}, err
	}
	identity, err := idempotency.NewIdentity(scope, key)
	if err != nil {
		return idempotency.Record{}, identifiers.UUID{}, err
	}
	recordID, err := identifiers.ParseIDKind(recordIDText, idempotency.RecordIDKind)
	if err != nil {
		return idempotency.Record{}, identifiers.UUID{}, err
	}
	fingerprint, err := identifiers.ParseDigest(fingerprintText)
	if err != nil {
		return idempotency.Record{}, identifiers.UUID{}, err
	}
	var requestID requestmeta.RequestID
	if requestIDText.Valid && requestIDText.String != "" {
		requestID, err = requestmeta.ParseRequestID(requestIDText.String)
		if err != nil {
			return idempotency.Record{}, identifiers.UUID{}, err
		}
	}
	var result idempotency.Result
	if len(resultJSON) > 0 && string(resultJSON) != "null" {
		if err := json.Unmarshal(resultJSON, &result); err != nil {
			return idempotency.Record{}, identifiers.UUID{}, err
		}
	}
	data := idempotency.RecordData{
		ID: recordID, Identity: identity, Fingerprint: fingerprint, State: idempotency.State(stateText),
		Result: result, RequestID: requestID, CreatedAt: createdAt, UpdatedAt: updatedAt, ExpiresAt: expiresAt, Version: version,
	}
	if leaseExpiresAt.Valid {
		data.LeaseExpiresAt = leaseExpiresAt.Time
	}
	record, err := idempotency.NewRecord(data)
	if err != nil {
		return idempotency.Record{}, identifiers.UUID{}, err
	}
	var token identifiers.UUID
	if leaseTokenText.Valid && leaseTokenText.String != "" {
		token, err = identifiers.ParseUUID(leaseTokenText.String)
		if err != nil {
			return idempotency.Record{}, identifiers.UUID{}, err
		}
	}
	return record, token, nil
}

func (store *Store) executor(ctx context.Context) databaseExecutor {
	if tx, ok := transaction.FromContext(ctx); ok {
		return tx
	}
	return store.db
}

func (store *Store) validateContext(ctx context.Context) error {
	if ctx == nil {
		return faults.Wrap(idempotency.ErrNilContext, faults.CodeInvalidArgument, "context must not be nil", faults.WithReason(idempotency.ReasonInvalidRequest), faults.WithOperation("idempotency.postgres"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if store == nil || store.db == nil || nilInterface(store.clock) || store.generator == nil || !validQualifiedIdentifier(store.table) {
		return faults.Wrap(idempotency.ErrNilStore, faults.CodeFailedPrecondition, "idempotency PostgreSQL store is not configured", faults.WithReason(idempotency.ReasonStoreFailed), faults.WithOperation("idempotency.postgres"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := ctx.Err(); err != nil {
		return faults.Wrap(err, faults.CodeOf(err), faults.PublicMessageOf(err), faults.WithReason("idempotency_context_done"), faults.WithOperation("idempotency.postgres"), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}

func provider(ctx context.Context, err error, operation string) error {
	qualified := sqlpostgres.Qualify(ctx, err, operation)
	return faults.Wrap(qualified, faults.CodeOf(qualified), "idempotency store operation failed", faults.WithReason(idempotency.ReasonStoreFailed), faults.WithOperation(operation), faults.WithFields(faults.FieldsOf(qualified)), faults.WithRetryPolicy(faults.RetryPolicyOf(qualified)), faults.WithContextMetadata(ctx))
}

func internal(ctx context.Context, err error, operation, reason string) error {
	return faults.Wrap(err, faults.CodeInternal, "idempotency store invariant failed", faults.WithReason(reason), faults.WithOperation(operation), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

func leaseLost(ctx context.Context, recordID identifiers.ID, operation string) error {
	return faults.Wrap(errors.Join(idempotency.ErrLeaseLost, ErrLeaseLost), faults.CodeConflict, "idempotency lease is no longer owned", faults.WithReason(idempotency.ReasonLeaseLost), faults.WithOperation(operation), faults.WithField("record_id", recordID.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

func notFound(ctx context.Context, identity idempotency.Identity) error {
	return faults.Wrap(errors.Join(idempotency.ErrNotFound, ErrRecordMissing), faults.CodeNotFound, "idempotency record was not found", faults.WithReason(idempotency.ReasonNotFound), faults.WithOperation("idempotency.postgres.Lookup"), faults.WithField("identity_digest", identity.Digest().String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
