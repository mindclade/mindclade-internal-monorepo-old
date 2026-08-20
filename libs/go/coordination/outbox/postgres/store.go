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
	"regexp"
	"strings"
	"time"

	"go.mindclade.dev/libs/go/coordination/outbox"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/requestmeta"
	"go.mindclade.dev/libs/go/storage/lease"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
)

const maximumPostgresIdentifierBytes = 63

var identifierPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

type Option func(*Store) error

func WithTokenGenerator(value func() (lease.Token, error)) Option {
	return func(store *Store) error {
		if value == nil {
			return faults.Wrap(outbox.ErrInvalidRequest, faults.CodeInvalidArgument, "outbox token generator is required", faults.WithReason("nil_outbox_token_generator"), faults.WithOperation("storage.outbox.postgres.WithTokenGenerator"), faults.WithRetryPolicy(faults.NoRetry()))
		}
		store.token = value
		return nil
	}
}

type Store struct {
	db    *sql.DB
	table string
	token func() (lease.Token, error)
}

var _ outbox.Store = (*Store)(nil)

func New(db *sql.DB, table string, options ...Option) (*Store, error) {
	if db == nil || !validTableName(table) {
		return nil, faults.Wrap(outbox.ErrInvalidRequest, faults.CodeInvalidArgument, "invalid PostgreSQL outbox configuration", faults.WithReason("invalid_postgres_outbox_config"), faults.WithOperation("storage.outbox.postgres.New"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	store := &Store{db: db, table: table, token: lease.NewToken}
	for _, option := range options {
		if option != nil {
			if err := option(store); err != nil {
				return nil, err
			}
		}
	}
	return store, nil
}

func DDL(table string) (string, error) {
	if !validTableName(table) {
		return "", faults.Wrap(outbox.ErrInvalidRequest, faults.CodeInvalidArgument, "invalid PostgreSQL outbox table", faults.WithReason("invalid_postgres_outbox_table"), faults.WithOperation("storage.outbox.postgres.DDL"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	pendingIndex := indexName(table, "pending_idx")
	claimIndex := indexName(table, "claim_idx")
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    message_id TEXT PRIMARY KEY,
    topic TEXT NOT NULL,
    partition_key TEXT NOT NULL DEFAULT '',
    content_type TEXT NOT NULL,
    payload BYTEA NOT NULL,
    headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    request_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    available_at TIMESTAMPTZ NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending','claimed','published','dead_letter')),
    version BIGINT NOT NULL CHECK (version > 0),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    claim_owner TEXT,
    claim_token TEXT,
    claimed_at TIMESTAMPTZ,
    claim_expires_at TIMESTAMPTZ,
    published_at TIMESTAMPTZ,
    dead_at TIMESTAMPTZ,
    last_error TEXT,
    CHECK ((state = 'claimed') = (claim_owner IS NOT NULL AND claim_token IS NOT NULL AND claimed_at IS NOT NULL AND claim_expires_at IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS %s ON %s (available_at, message_id) WHERE state = 'pending';
CREATE INDEX IF NOT EXISTS %s ON %s (claim_expires_at, message_id) WHERE state = 'claimed';`, table, pendingIndex, table, claimIndex, table), nil
}

type dbtx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (store *Store) executor(ctx context.Context) dbtx {
	if tx, ok := transaction.FromContext(ctx); ok {
		return tx
	}
	return store.db
}

func (store *Store) Append(ctx context.Context, message outbox.Message) error {
	if ctx == nil || store == nil || store.db == nil {
		return invalid(ctx, "storage.outbox.postgres.Append")
	}
	if err := message.Validate(); err != nil {
		return err
	}
	headers, err := json.Marshal(message.Headers())
	if err != nil {
		return invalid(ctx, "storage.outbox.postgres.Append")
	}
	metadata, err := json.Marshal(message.Request())
	if err != nil {
		return invalid(ctx, "storage.outbox.postgres.Append")
	}
	query := fmt.Sprintf(`INSERT INTO %s (
message_id, topic, partition_key, content_type, payload, headers, request_metadata,
created_at, available_at, state, version, attempts
) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,'pending',1,0)`, store.table)
	_, err = store.executor(ctx).ExecContext(ctx, query,
		message.ID().String(), message.Topic(), message.PartitionKey(), message.ContentType(), message.Payload(), headers, metadata, message.CreatedAt(), message.AvailableAt())
	if err != nil {
		qualified := sqlpostgres.Qualify(ctx, err, "storage.outbox.postgres.Append")
		if faults.IsCode(qualified, faults.CodeAlreadyExists) {
			return faults.Wrap(errors.Join(outbox.ErrAlreadyExists, qualified), faults.CodeAlreadyExists, "outbox message already exists", faults.WithReason("outbox_message_exists"), faults.WithOperation("storage.outbox.postgres.Append"), faults.WithField("outbox_message_id", message.ID().String()), faults.WithRetryPolicy(faults.NoRetry()), faults.WithContextMetadata(ctx))
		}
		return qualify(ctx, qualified, "storage.outbox.postgres.Append")
	}
	return nil
}

func (store *Store) Claim(ctx context.Context, request outbox.ClaimRequest) ([]outbox.Claim, error) {
	if ctx == nil || store == nil || store.db == nil || store.token == nil {
		return nil, invalid(ctx, "storage.outbox.postgres.Claim")
	}
	if _, nested := transaction.FromContext(ctx); nested {
		return nil, faults.Wrap(outbox.ErrInvalidRequest, faults.CodeFailedPrecondition, "outbox claims may not run inside a caller transaction", faults.WithReason("outbox_claim_nested_transaction"), faults.WithOperation("storage.outbox.postgres.Claim"), faults.WithRetryPolicy(faults.NoRetry()), faults.WithContextMetadata(ctx))
	}
	request = request.Normalized()
	if err := request.Validate(); err != nil {
		return nil, err
	}
	token, err := store.token()
	if err != nil {
		return nil, faults.Wrap(err, faults.CodeInternal, "unable to generate outbox claim token", faults.WithReason("outbox_token_generation_failed"), faults.WithOperation("storage.outbox.postgres.Claim"), faults.WithRetryPolicy(faults.BackoffRetry(3)), faults.WithContextMetadata(ctx))
	}
	topics := strings.Join(request.Topics, ",")
	query := fmt.Sprintf(`WITH candidates AS (
    SELECT message_id
    FROM %s
    WHERE (
        (state = 'pending' AND available_at <= clock_timestamp())
        OR (state = 'claimed' AND claim_expires_at <= clock_timestamp())
    )
      AND ($1 = '' OR topic = ANY(string_to_array($1, ',')))
    ORDER BY available_at, message_id
    FOR UPDATE SKIP LOCKED
    LIMIT $2
)
UPDATE %s AS outbox
SET state = 'claimed',
    claim_owner = $3,
    claim_token = $4,
    claimed_at = clock_timestamp(),
    claim_expires_at = clock_timestamp() + ($5 * interval '1 microsecond'),
    attempts = attempts + 1,
    version = version + 1
FROM candidates
WHERE outbox.message_id = candidates.message_id
RETURNING %s`, store.table, store.table, returningColumns("outbox"))
	rows, err := store.db.QueryContext(ctx, query, topics, request.Limit, request.Owner, token.String(), request.LeaseDuration.Microseconds())
	if err != nil {
		return nil, qualify(ctx, sqlpostgres.Qualify(ctx, err, "storage.outbox.postgres.Claim"), "storage.outbox.postgres.Claim")
	}
	defer rows.Close()
	claims := make([]outbox.Claim, 0, request.Limit)
	for rows.Next() {
		record, claim, scanErr := scanClaim(rows)
		if scanErr != nil {
			return nil, qualify(ctx, scanErr, "storage.outbox.postgres.Claim.scan")
		}
		if record.State != outbox.StateClaimed || !claim.Token().Equal(token) {
			return nil, faults.Wrap(outbox.ErrInvalidClaim, faults.CodeInternal, "PostgreSQL outbox returned an invalid claim", faults.WithReason("outbox_store_contract_failed"), faults.WithOperation("storage.outbox.postgres.Claim"), faults.WithRetryPolicy(faults.NoRetry()))
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, qualify(ctx, sqlpostgres.Qualify(ctx, err, "storage.outbox.postgres.Claim"), "storage.outbox.postgres.Claim")
	}
	return claims, nil
}

func (store *Store) Renew(ctx context.Context, claim outbox.Claim, ttl time.Duration) (outbox.Claim, error) {
	if ctx == nil || store == nil || store.db == nil || ttl <= 0 {
		return outbox.Claim{}, invalid(ctx, "storage.outbox.postgres.Renew")
	}
	if err := claim.Validate(); err != nil {
		return outbox.Claim{}, err
	}
	query := fmt.Sprintf(`UPDATE %s AS outbox
SET claim_expires_at = clock_timestamp() + ($5 * interval '1 microsecond'), version = version + 1
WHERE message_id = $1 AND state = 'claimed' AND claim_token = $2 AND claim_owner = $3 AND version = $4 AND claim_expires_at > clock_timestamp()
RETURNING %s`, store.table, returningColumns("outbox"))
	record, renewed, err := scanClaim(store.db.QueryRowContext(ctx, query, claim.Message().ID().String(), claim.Token().String(), claim.Owner(), claim.Version(), ttl.Microseconds()))
	if errors.Is(err, sql.ErrNoRows) {
		return outbox.Claim{}, claimLost(ctx, claim, "storage.outbox.postgres.Renew")
	}
	if err != nil {
		return outbox.Claim{}, qualify(ctx, sqlpostgres.Qualify(ctx, err, "storage.outbox.postgres.Renew"), "storage.outbox.postgres.Renew")
	}
	if record.State != outbox.StateClaimed {
		return outbox.Claim{}, faults.Wrap(outbox.ErrInvalidClaim, faults.CodeInternal, "PostgreSQL outbox renewed an invalid claim", faults.WithReason("outbox_store_contract_failed"), faults.WithOperation("storage.outbox.postgres.Renew"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return renewed, nil
}

func (store *Store) MarkPublished(ctx context.Context, claim outbox.Claim, publishedAt time.Time) error {
	if publishedAt.IsZero() {
		return invalid(ctx, "storage.outbox.postgres.MarkPublished")
	}
	return store.transition(ctx, claim, `state = 'published', published_at = $5, last_error = NULL`, publishedAt.Round(0).UTC(), "storage.outbox.postgres.MarkPublished")
}

func (store *Store) Reschedule(ctx context.Context, claim outbox.Claim, availableAt time.Time, reason string) error {
	if availableAt.IsZero() || strings.TrimSpace(reason) == "" {
		return invalid(ctx, "storage.outbox.postgres.Reschedule")
	}
	return store.transition(ctx, claim, `state = 'pending', available_at = $5, last_error = $6`, availableAt.Round(0).UTC(), "storage.outbox.postgres.Reschedule", truncate(reason, 256))
}

func (store *Store) DeadLetter(ctx context.Context, claim outbox.Claim, deadAt time.Time, reason string) error {
	if deadAt.IsZero() || strings.TrimSpace(reason) == "" {
		return invalid(ctx, "storage.outbox.postgres.DeadLetter")
	}
	return store.transition(ctx, claim, `state = 'dead_letter', dead_at = $5, last_error = $6`, deadAt.Round(0).UTC(), "storage.outbox.postgres.DeadLetter", truncate(reason, 256))
}

func (store *Store) transition(ctx context.Context, claim outbox.Claim, assignment string, timestamp time.Time, operation string, additional ...any) error {
	if ctx == nil || store == nil || store.db == nil {
		return invalid(ctx, operation)
	}
	if err := claim.Validate(); err != nil {
		return err
	}
	query := fmt.Sprintf(`UPDATE %s SET %s,
claim_owner = NULL, claim_token = NULL, claimed_at = NULL, claim_expires_at = NULL, version = version + 1
WHERE message_id = $1 AND state = 'claimed' AND claim_token = $2 AND claim_owner = $3 AND version = $4 AND claim_expires_at > clock_timestamp()`, store.table, assignment)
	arguments := []any{claim.Message().ID().String(), claim.Token().String(), claim.Owner(), claim.Version(), timestamp}
	arguments = append(arguments, additional...)
	result, err := store.db.ExecContext(ctx, query, arguments...)
	if err != nil {
		return qualify(ctx, sqlpostgres.Qualify(ctx, err, operation), operation)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return qualify(ctx, err, operation)
	}
	if affected != 1 {
		return claimLost(ctx, claim, operation)
	}
	return nil
}

func (store *Store) Lookup(ctx context.Context, identifier string) (outbox.Record, error) {
	if ctx == nil || store == nil || store.db == nil || strings.TrimSpace(identifier) == "" {
		return outbox.Record{}, invalid(ctx, "storage.outbox.postgres.Lookup")
	}
	query := fmt.Sprintf(`SELECT %s FROM %s AS outbox WHERE message_id = $1`, returningColumns("outbox"), store.table)
	record, _, err := scanClaim(store.db.QueryRowContext(ctx, query, identifier))
	if errors.Is(err, sql.ErrNoRows) {
		return outbox.Record{}, faults.Wrap(outbox.ErrNotFound, faults.CodeNotFound, "outbox message not found", faults.WithReason("outbox_message_not_found"), faults.WithOperation("storage.outbox.postgres.Lookup"), faults.WithField("outbox_message_id", identifier), faults.WithRetryPolicy(faults.NoRetry()), faults.WithContextMetadata(ctx))
	}
	if err != nil {
		return outbox.Record{}, qualify(ctx, sqlpostgres.Qualify(ctx, err, "storage.outbox.postgres.Lookup"), "storage.outbox.postgres.Lookup")
	}
	return record, nil
}

type scanner interface{ Scan(...any) error }

func scanClaim(row scanner) (outbox.Record, outbox.Claim, error) {
	var (
		idText, topic, partitionKey, contentType, stateText string
		payload, headersJSON, requestJSON                   []byte
		createdAt, availableAt                              time.Time
		version                                             int64
		attempts                                            int64
		claimOwner, claimToken, lastError                   sql.NullString
		claimedAt, claimExpires, publishedAt, deadAt        sql.NullTime
	)
	if err := row.Scan(&idText, &topic, &partitionKey, &contentType, &payload, &headersJSON, &requestJSON, &createdAt, &availableAt, &stateText, &version, &attempts, &claimOwner, &claimToken, &claimedAt, &claimExpires, &publishedAt, &deadAt, &lastError); err != nil {
		return outbox.Record{}, outbox.Claim{}, err
	}
	id, err := identifiers.ParseIDKind(idText, outbox.MessageIDKind)
	if err != nil {
		return outbox.Record{}, outbox.Claim{}, err
	}
	var headers map[string]string
	if len(headersJSON) > 0 {
		if err := json.Unmarshal(headersJSON, &headers); err != nil {
			return outbox.Record{}, outbox.Claim{}, err
		}
	}
	var metadata requestmeta.Metadata
	if len(requestJSON) > 0 {
		if err := json.Unmarshal(requestJSON, &metadata); err != nil {
			return outbox.Record{}, outbox.Claim{}, err
		}
	}
	message, err := outbox.NewMessage(id, topic, partitionKey, contentType, payload, headers, metadata, createdAt, availableAt)
	if err != nil {
		return outbox.Record{}, outbox.Claim{}, err
	}
	if version <= 0 || attempts < 0 || attempts > outbox.MaximumAttempts {
		return outbox.Record{}, outbox.Claim{}, outbox.ErrInvalidMessage
	}
	record := outbox.Record{Message: message, State: outbox.State(stateText), Version: uint64(version), Attempts: uint32(attempts), ClaimOwner: claimOwner.String, ClaimToken: claimToken.String, LastError: lastError.String}
	if claimedAt.Valid {
		record.ClaimedAt = claimedAt.Time.UTC()
	}
	if claimExpires.Valid {
		record.ClaimExpires = claimExpires.Time.UTC()
	}
	if publishedAt.Valid {
		record.PublishedAt = publishedAt.Time.UTC()
	}
	if deadAt.Valid {
		record.DeadAt = deadAt.Time.UTC()
	}
	if err := record.Validate(); err != nil {
		return outbox.Record{}, outbox.Claim{}, err
	}
	if record.State != outbox.StateClaimed {
		return record, outbox.Claim{}, nil
	}
	token, err := lease.ParseToken(record.ClaimToken)
	if err != nil {
		return outbox.Record{}, outbox.Claim{}, err
	}
	claim, err := outbox.NewClaim(record.Message, token, record.ClaimOwner, record.Version, record.Attempts, record.ClaimedAt, record.ClaimExpires)
	return record, claim, err
}

func returningColumns(alias string) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	columns := []string{"message_id", "topic", "partition_key", "content_type", "payload", "headers", "request_metadata", "created_at", "available_at", "state", "version", "attempts", "claim_owner", "claim_token", "claimed_at", "claim_expires_at", "published_at", "dead_at", "last_error"}
	for index := range columns {
		columns[index] = prefix + columns[index]
	}
	return strings.Join(columns, ", ")
}

func validTableName(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) < 1 || len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if len(part) == 0 || len(part) > maximumPostgresIdentifierBytes || !identifierPattern.MatchString(part) {
			return false
		}
	}
	return true
}

func indexName(table, suffix string) string {
	base := strings.ReplaceAll(table, ".", "_")
	maximumBase := maximumPostgresIdentifierBytes - len(suffix) - 1
	if len(base) > maximumBase {
		base = base[:maximumBase]
	}
	return base + "_" + suffix
}

func invalid(ctx context.Context, operation string) error {
	return faults.Wrap(outbox.ErrInvalidRequest, faults.CodeInvalidArgument, "invalid PostgreSQL outbox request", faults.WithReason("invalid_postgres_outbox_request"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()), faults.WithContextMetadata(ctx))
}
func claimLost(ctx context.Context, claim outbox.Claim, operation string) error {
	return faults.Wrap(outbox.ErrClaimLost, faults.CodeConflict, "outbox claim is stale or expired", faults.WithReason(outbox.ReasonClaimLost), faults.WithOperation(operation), faults.WithField("outbox_message_id", claim.Message().ID().String()), faults.WithRetryPolicy(faults.NoRetry()), faults.WithContextMetadata(ctx))
}
func qualify(ctx context.Context, err error, operation string) error {
	if err == nil {
		return nil
	}
	code := faults.CodeOf(err)
	if code == faults.CodeUnknown {
		code = faults.CodeUnavailable
	}
	reason := faults.ReasonOf(err)
	if reason == "" {
		reason = outbox.ReasonStoreFailed
	}
	policy := faults.RetryPolicyOf(err)
	if !policy.Specified() && code == faults.CodeUnavailable {
		policy = faults.BackoffRetry(5)
	}
	return faults.Wrap(errors.Join(outbox.ErrUnavailable, err), code, "outbox store operation failed", faults.WithReason(reason), faults.WithOperation(operation), faults.WithRetryPolicy(policy), faults.WithFields(faults.FieldsOf(err)), faults.WithContextMetadata(ctx))
}
func truncate(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}
