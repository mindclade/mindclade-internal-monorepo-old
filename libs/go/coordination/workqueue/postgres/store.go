// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"go.mindclade.dev/libs/go/coordination"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/requestmeta"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
	"time"
)

type Store struct {
	db    *sql.DB
	table string
}

func New(db *sql.DB, table string) (*Store, error) {
	if db == nil {
		return nil, invalid(nil, "workqueue.postgres.New")
	}
	name, err := sqlpostgres.QualifiedIdentifier(table)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, table: name}, nil
}
func DDL(table string) (string, error) {
	name, err := sqlpostgres.QualifiedIdentifier(table)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
item_id TEXT PRIMARY KEY, queue TEXT NOT NULL, payload JSONB NOT NULL, priority INTEGER NOT NULL,
available_at TIMESTAMPTZ NOT NULL, max_attempts INTEGER NOT NULL CHECK(max_attempts>0), created_at TIMESTAMPTZ NOT NULL,
request_metadata JSONB NOT NULL DEFAULT '{}'::jsonb, state TEXT NOT NULL CHECK(state IN ('pending','leased','completed','failed','cancelled')),
attempts INTEGER NOT NULL DEFAULT 0, fence BIGINT NOT NULL DEFAULT 0, updated_at TIMESTAMPTZ NOT NULL, completed_at TIMESTAMPTZ,
result_content_type TEXT, result_payload BYTEA, last_error TEXT, claim_token TEXT, claim_owner TEXT, claimed_at TIMESTAMPTZ, claim_expires_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS %s_pending_idx ON %s(queue,priority DESC,available_at,item_id) WHERE state='pending';
CREATE INDEX IF NOT EXISTS %s_lease_idx ON %s(claim_expires_at,item_id) WHERE state='leased';`, name, indexBase(name), name, indexBase(name), name), nil
}
func indexBase(table string) string {
	for i := len(table) - 1; i >= 0; i-- {
		if table[i] == '.' {
			return table[i+1:]
		}
	}
	return table
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (store *Store) exec(ctx context.Context) execer {
	if tx, ok := transaction.FromContext(ctx); ok {
		return tx
	}
	return store.db
}
func (store *Store) Enqueue(ctx context.Context, item workqueue.Item) error {
	if ctx == nil || store == nil || store.db == nil {
		return invalid(ctx, "workqueue.postgres.Enqueue")
	}
	if err := item.Validate(); err != nil {
		return err
	}
	metadata, _ := json.Marshal(item.Request)
	query := fmt.Sprintf(`INSERT INTO %s(item_id,queue,payload,priority,available_at,max_attempts,created_at,request_metadata,state,attempts,fence,updated_at) VALUES($1,$2,$3::jsonb,$4,$5,$6,$7,$8::jsonb,'pending',0,0,$7)`, store.table)
	_, err := store.exec(ctx).ExecContext(ctx, query, item.ID.String(), item.Queue, []byte(item.Payload), item.Priority, item.AvailableAt, item.MaxAttempts, item.CreatedAt, metadata)
	if err != nil {
		qualified := sqlpostgres.Qualify(ctx, err, "workqueue.postgres.Enqueue")
		if faults.IsCode(qualified, faults.CodeAlreadyExists) {
			return faults.Wrap(errors.Join(workqueue.ErrAlreadyExists, qualified), faults.CodeAlreadyExists, "work item already exists", faults.WithReason("work_item_exists"), faults.WithField("work_item_id", item.ID.String()), faults.WithRetryPolicy(faults.NoRetry()))
		}
		return qualified
	}
	return nil
}
func (store *Store) Claim(ctx context.Context, request workqueue.ClaimRequest) ([]workqueue.Claim, error) {
	if ctx == nil || store == nil || store.db == nil {
		return nil, invalid(ctx, "workqueue.postgres.Claim")
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	tx, owned, err := store.tx(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	now := time.Now().UTC()
	queues := request.Queues
	if len(queues) == 0 {
		queues = []string{""}
	}
	query := fmt.Sprintf(`WITH candidates AS (SELECT item_id FROM %s WHERE ((state='pending' AND available_at <= $1) OR (state='leased' AND claim_expires_at <= $1)) AND ($2 OR queue = ANY($3)) ORDER BY priority DESC,available_at,item_id FOR UPDATE SKIP LOCKED LIMIT $4) UPDATE %s w SET state='leased',attempts=w.attempts+1,fence=w.fence+1,updated_at=$1,claim_owner=$5,claim_token=$6,claimed_at=$1,claim_expires_at=$7 FROM candidates c WHERE w.item_id=c.item_id RETURNING w.item_id,w.queue,w.payload,w.priority,w.available_at,w.max_attempts,w.created_at,w.request_metadata,w.state,w.attempts,w.fence,w.updated_at,w.completed_at,w.result_content_type,w.result_payload,w.last_error,w.claim_token,w.claim_owner,w.claimed_at,w.claim_expires_at`, store.table, store.table)
	token, err := identifiers.NewUUIDv4()
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, query, now, len(request.Queues) == 0, queues, request.Limit, request.Owner, token.String(), now.Add(request.LeaseDuration))
	if err != nil {
		return nil, sqlpostgres.Qualify(ctx, err, "workqueue.postgres.Claim")
	}
	defer rows.Close()
	var claims []workqueue.Claim
	for rows.Next() {
		record, claim, err := scan(rows)
		if err != nil {
			return nil, err
		}
		claims = append(claims, workqueue.Claim{Record: record, Ownership: claim})
	}
	if err = rows.Err(); err != nil {
		return nil, sqlpostgres.Qualify(ctx, err, "workqueue.postgres.Claim")
	}
	if owned {
		if err = tx.Commit(); err != nil {
			return nil, sqlpostgres.Qualify(ctx, err, "workqueue.postgres.Claim.commit")
		}
	}
	return claims, nil
}
func (store *Store) Renew(ctx context.Context, claim workqueue.Claim, duration time.Duration) (workqueue.Claim, error) {
	if err := claim.Validate(); err != nil {
		return workqueue.Claim{}, err
	}
	if duration <= 0 {
		return workqueue.Claim{}, invalid(ctx, "workqueue.postgres.Renew")
	}
	now := time.Now().UTC()
	query := fmt.Sprintf(`UPDATE %s SET updated_at=$1,claim_expires_at=$2 WHERE item_id=$3 AND state='leased' AND claim_token=$4 AND fence=$5 AND claim_expires_at>$1`, store.table)
	result, err := store.exec(ctx).ExecContext(ctx, query, now, now.Add(duration), claim.Record.Item.ID.String(), claim.Ownership.Token.String(), claim.Ownership.Fence)
	if err != nil {
		return workqueue.Claim{}, sqlpostgres.Qualify(ctx, err, "workqueue.postgres.Renew")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return workqueue.Claim{}, sqlpostgres.Qualify(ctx, err, "workqueue.postgres.Renew")
	}
	if affected != 1 {
		return workqueue.Claim{}, lost(ctx, claim.Record.Item.ID, "workqueue.postgres.Renew")
	}
	next, err := coordination.ClaimFromToken(claim.Ownership.ResourceID, claim.Ownership.Token, claim.Ownership.Owner, claim.Ownership.Fence, claim.Ownership.AcquiredAt, now.Add(duration))
	if err != nil {
		return workqueue.Claim{}, err
	}
	claim.Ownership = next
	claim.Record.UpdatedAt = now
	return claim, nil
}
func (store *Store) Complete(ctx context.Context, claim workqueue.Claim, result workqueue.Result, at time.Time) error {
	if err := result.Validate(); err != nil {
		return err
	}
	query := fmt.Sprintf(`UPDATE %s SET state='completed',updated_at=$1,completed_at=$1,result_content_type=$2,result_payload=$3,last_error=NULL,claim_token=NULL,claim_owner=NULL,claimed_at=NULL,claim_expires_at=NULL WHERE item_id=$4 AND state='leased' AND claim_token=$5 AND fence=$6 AND claim_expires_at>$1`, store.table)
	return store.transition(ctx, query, at, result.ContentType, result.Payload, claim.Record.Item.ID.String(), claim.Ownership.Token.String(), claim.Ownership.Fence)
}
func (store *Store) Fail(ctx context.Context, claim workqueue.Claim, failure workqueue.Failure, at time.Time) error {
	if err := failure.Validate(); err != nil {
		return err
	}
	state := "pending"
	var completed any
	available := failure.RetryAt
	if failure.Terminal {
		state = "failed"
		completed = at
		available = claim.Record.Item.AvailableAt
	}
	query := fmt.Sprintf(`UPDATE %s SET state=$1,updated_at=$2,completed_at=$3,available_at=$4,last_error=$5,claim_token=NULL,claim_owner=NULL,claimed_at=NULL,claim_expires_at=NULL WHERE item_id=$6 AND state='leased' AND claim_token=$7 AND fence=$8 AND claim_expires_at>$2`, store.table)
	return store.transition(ctx, query, state, at, completed, available, failure.Reason, claim.Record.Item.ID.String(), claim.Ownership.Token.String(), claim.Ownership.Fence)
}
func (store *Store) Cancel(ctx context.Context, id identifiers.ID, reason string, at time.Time) error {
	if ctx == nil || id.IsZero() || reason == "" || at.IsZero() {
		return invalid(ctx, "workqueue.postgres.Cancel")
	}
	query := fmt.Sprintf(`UPDATE %s SET state='cancelled',updated_at=$1,completed_at=$1,last_error=$2,claim_token=NULL,claim_owner=NULL,claimed_at=NULL,claim_expires_at=NULL WHERE item_id=$3 AND state IN ('pending','leased')`, store.table)
	result, err := store.exec(ctx).ExecContext(ctx, query, at, reason, id.String())
	if err != nil {
		return sqlpostgres.Qualify(ctx, err, "workqueue.postgres.Cancel")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return sqlpostgres.Qualify(ctx, err, "workqueue.postgres.Cancel")
	}
	if affected != 1 {
		return workqueue.ErrTerminal
	}
	return nil
}
func (store *Store) Lookup(ctx context.Context, id identifiers.ID) (workqueue.Record, error) {
	if ctx == nil || store == nil || id.IsZero() {
		return workqueue.Record{}, invalid(ctx, "workqueue.postgres.Lookup")
	}
	query := fmt.Sprintf(`SELECT item_id,queue,payload,priority,available_at,max_attempts,created_at,request_metadata,state,attempts,fence,updated_at,completed_at,result_content_type,result_payload,last_error,claim_token,claim_owner,claimed_at,claim_expires_at FROM %s WHERE item_id=$1`, store.table)
	record, _, err := scan(store.exec(ctx).QueryRowContext(ctx, query, id.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return workqueue.Record{}, faults.Wrap(workqueue.ErrNotFound, faults.CodeNotFound, "work item not found", faults.WithReason("work_item_not_found"), faults.WithField("work_item_id", id.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return record, err
}
func (store *Store) transition(ctx context.Context, query string, args ...any) error {
	result, err := store.exec(ctx).ExecContext(ctx, query, args...)
	if err != nil {
		return sqlpostgres.Qualify(ctx, err, "workqueue.postgres.transition")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return sqlpostgres.Qualify(ctx, err, "workqueue.postgres.transition")
	}
	if affected != 1 {
		return faults.Wrap(workqueue.ErrLeaseLost, faults.CodeAborted, "work lease was lost", faults.WithReason("work_lease_lost"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}
func (store *Store) tx(ctx context.Context) (*sql.Tx, bool, error) {
	if tx, ok := transaction.FromContext(ctx); ok {
		return tx, false, nil
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, sqlpostgres.Qualify(ctx, err, "workqueue.postgres.begin")
	}
	return tx, true, nil
}

type scanner interface{ Scan(...any) error }

func scan(row scanner) (workqueue.Record, coordination.Claim, error) {
	var idText, queue string
	var payload, metadata []byte
	var priority int32
	var available, created, updated time.Time
	var maxAttempts, attempts uint32
	var state workqueue.State
	var fence uint64
	var completed sql.NullTime
	var content, lastError, tokenText, owner sql.NullString
	var resultPayload []byte
	var claimed, expires sql.NullTime
	if err := row.Scan(&idText, &queue, &payload, &priority, &available, &maxAttempts, &created, &metadata, &state, &attempts, &fence, &updated, &completed, &content, &resultPayload, &lastError, &tokenText, &owner, &claimed, &expires); err != nil {
		return workqueue.Record{}, coordination.Claim{}, err
	}
	id, err := identifiers.ParseIDKind(idText, workqueue.ItemIDKind)
	if err != nil {
		return workqueue.Record{}, coordination.Claim{}, err
	}
	var meta requestmeta.Metadata
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &meta); err != nil {
			return workqueue.Record{}, coordination.Claim{}, fmt.Errorf("decode request metadata: %w", err)
		}
	}
	item, err := workqueue.ItemFromID(id, queue, payload, priority, available, maxAttempts, meta)
	if err != nil {
		return workqueue.Record{}, coordination.Claim{}, err
	}
	item.CreatedAt = created
	record := workqueue.Record{Item: item, State: state, Attempts: attempts, Fence: fence, UpdatedAt: updated, Result: workqueue.Result{ContentType: content.String, Payload: resultPayload}, LastError: lastError.String}
	if completed.Valid {
		record.CompletedAt = completed.Time
	}
	var claim coordination.Claim
	if state == workqueue.StateLeased && tokenText.Valid && owner.Valid && claimed.Valid && expires.Valid {
		token, parseErr := identifiers.ParseUUID(tokenText.String)
		if parseErr != nil {
			return workqueue.Record{}, coordination.Claim{}, parseErr
		}
		claim, err = coordination.ClaimFromToken(id, token, owner.String, fence, claimed.Time, expires.Time)
		if err != nil {
			return workqueue.Record{}, coordination.Claim{}, err
		}
	}
	return record, claim, nil
}
func invalid(ctx context.Context, op string) error {
	return faults.Wrap(workqueue.ErrInvalidRequest, faults.CodeInvalidArgument, "invalid PostgreSQL work request", faults.WithReason("invalid_work_request"), faults.WithOperation(op), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
func lost(ctx context.Context, id identifiers.ID, op string) error {
	return faults.Wrap(workqueue.ErrLeaseLost, faults.CodeAborted, "work lease was lost", faults.WithReason("work_lease_lost"), faults.WithOperation(op), faults.WithField("work_item_id", id.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
