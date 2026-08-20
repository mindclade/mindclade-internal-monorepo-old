// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/storage/lease"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
)

const maximumPostgresIdentifierBytes = 63

var identifierPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

type Option func(*Store) error

func WithTokenGenerator(generator func() (lease.Token, error)) Option {
	return func(store *Store) error {
		if generator == nil {
			return errors.New("postgres lease: nil token generator")
		}
		store.generateToken = generator
		return nil
	}
}

type Store struct {
	db            *sql.DB
	table         string
	generateToken func() (lease.Token, error)
}

var _ lease.Store = (*Store)(nil)

func New(db *sql.DB, table string, options ...Option) (*Store, error) {
	if db == nil || !validTableName(table) {
		return nil, faults.New(faults.CodeInvalidArgument, "invalid PostgreSQL lease store configuration", faults.WithReason("invalid_postgres_lease_config"), faults.WithOperation("storage.lease.postgres.New"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	store := &Store{db: db, table: table, generateToken: lease.NewToken}
	for _, option := range options {
		if option != nil {
			if err := option(store); err != nil {
				return nil, faults.Wrap(err, faults.CodeInvalidArgument, "invalid PostgreSQL lease store configuration", faults.WithReason("invalid_postgres_lease_option"), faults.WithOperation("storage.lease.postgres.New"), faults.WithRetryPolicy(faults.NoRetry()))
			}
		}
	}
	return store, nil
}

func DDL(table string) (string, error) {
	if !validTableName(table) {
		return "", faults.New(faults.CodeInvalidArgument, "invalid PostgreSQL lease table", faults.WithReason("invalid_postgres_lease_table"), faults.WithOperation("storage.lease.postgres.DDL"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    lease_key TEXT PRIMARY KEY,
    token TEXT NOT NULL,
    owner TEXT NOT NULL,
    version BIGINT NOT NULL CHECK (version > 0),
    acquired_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    CHECK (expires_at > acquired_at)
);
CREATE INDEX IF NOT EXISTS %s ON %s (expires_at);`, table, indexName(table), table), nil
}

func (store *Store) Acquire(ctx context.Context, request lease.AcquireRequest) (lease.Lease, error) {
	const operation = "storage.lease.postgres.Acquire"
	if ctx == nil || store == nil || store.db == nil {
		return lease.Lease{}, invalidRequest(operation)
	}
	if err := request.Validate(); err != nil {
		return lease.Lease{}, err
	}
	if err := ctx.Err(); err != nil {
		return lease.Lease{}, sqlpostgres.Qualify(ctx, err, operation)
	}
	token, err := store.generateToken()
	if err != nil {
		return lease.Lease{}, faults.Wrap(err, faults.CodeInternal, "unable to generate lease token", faults.WithReason("lease_token_generation_failed"), faults.WithOperation(operation), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.BackoffRetry(3)))
	}
	if token.IsZero() {
		return lease.Lease{}, faults.New(faults.CodeInternal, "lease token generator returned an empty token", faults.WithReason("empty_lease_token"), faults.WithOperation(operation), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}

	value, err := scanLease(store.db.QueryRowContext(
		ctx,
		store.acquireQuery(),
		request.Key.String(),
		token.String(),
		request.Owner,
		durationMicroseconds(request.TTL),
	))
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return lease.Lease{}, sqlpostgres.Qualify(ctx, err, operation)
	}

	current, remaining, lookupErr := store.lookupAnyWithRemaining(ctx, request.Key)
	if errors.Is(lookupErr, sql.ErrNoRows) {
		return lease.Lease{}, faults.Wrap(lease.ErrHeld, faults.CodeAborted, "lease acquisition raced with another owner", faults.WithReason("lease_acquire_raced"), faults.WithOperation(operation), faults.WithField("lease_key", request.Key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.ImmediateRetry(0)))
	}
	if lookupErr != nil {
		return lease.Lease{}, sqlpostgres.Qualify(ctx, lookupErr, operation+".Lookup")
	}
	policy := faults.ImmediateRetry(0)
	if remaining > 0 {
		policy = faults.DelayedRetry(remaining, 0)
	}
	return lease.Lease{}, faults.Wrap(lease.ErrHeld, faults.CodeConflict, "lease is already held", faults.WithReason("lease_held"), faults.WithOperation(operation), faults.WithFields(faults.Fields{"lease_key": request.Key.String(), "lease_owner": current.Owner}), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(policy))
}

func (store *Store) Renew(ctx context.Context, current lease.Lease, ttl time.Duration) (lease.Lease, error) {
	const operation = "storage.lease.postgres.Renew"
	if ctx == nil || store == nil || store.db == nil || ttl <= 0 {
		return lease.Lease{}, invalidRequest(operation)
	}
	if err := current.Validate(); err != nil {
		return lease.Lease{}, err
	}
	value, err := scanLease(store.db.QueryRowContext(
		ctx,
		store.renewQuery(),
		current.Key.String(),
		current.Token.String(),
		current.Version,
		durationMicroseconds(ttl),
	))
	if errors.Is(err, sql.ErrNoRows) {
		return lease.Lease{}, faults.Wrap(lease.ErrStale, faults.CodeConflict, "lease ownership is stale", faults.WithReason("stale_lease"), faults.WithOperation(operation), faults.WithField("lease_key", current.Key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err != nil {
		return lease.Lease{}, sqlpostgres.Qualify(ctx, err, operation)
	}
	return value, nil
}

func (store *Store) Release(ctx context.Context, current lease.Lease) error {
	const operation = "storage.lease.postgres.Release"
	if ctx == nil || store == nil || store.db == nil {
		return invalidRequest(operation)
	}
	if err := current.Validate(); err != nil {
		return err
	}
	query := fmt.Sprintf(`DELETE FROM %s WHERE lease_key = $1 AND token = $2 AND version = $3`, store.table)
	result, err := store.db.ExecContext(ctx, query, current.Key.String(), current.Token.String(), current.Version)
	if err != nil {
		return sqlpostgres.Qualify(ctx, err, operation)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return sqlpostgres.Qualify(ctx, err, operation+".RowsAffected")
	}
	if affected != 1 {
		return faults.Wrap(lease.ErrStale, faults.CodeConflict, "lease ownership is stale", faults.WithReason("stale_lease"), faults.WithOperation(operation), faults.WithField("lease_key", current.Key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}

func (store *Store) Lookup(ctx context.Context, key lease.Key) (lease.Lease, error) {
	const operation = "storage.lease.postgres.Lookup"
	if ctx == nil || store == nil || store.db == nil {
		return lease.Lease{}, invalidRequest(operation)
	}
	if err := key.Validate(); err != nil {
		return lease.Lease{}, err
	}
	value, err := scanLease(store.db.QueryRowContext(ctx, store.lookupQuery(), key.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return lease.Lease{}, faults.Wrap(lease.ErrNotFound, faults.CodeNotFound, "lease not found", faults.WithReason("lease_not_found"), faults.WithOperation(operation), faults.WithField("lease_key", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err != nil {
		return lease.Lease{}, sqlpostgres.Qualify(ctx, err, operation)
	}
	return value, nil
}

func (store *Store) acquireQuery() string {
	return fmt.Sprintf(`WITH timing AS (SELECT clock_timestamp() AS now)
INSERT INTO %s (lease_key, token, owner, version, acquired_at, expires_at)
SELECT $1, $2, $3, 1, timing.now, timing.now + ($4 * interval '1 microsecond')
FROM timing
ON CONFLICT (lease_key) DO UPDATE SET
    token = EXCLUDED.token,
    owner = EXCLUDED.owner,
    version = %s.version + 1,
    acquired_at = EXCLUDED.acquired_at,
    expires_at = EXCLUDED.expires_at
WHERE %s.expires_at <= EXCLUDED.acquired_at
RETURNING lease_key, token, owner, version, acquired_at, expires_at`, store.table, store.table, store.table)
}

func (store *Store) renewQuery() string {
	return fmt.Sprintf(`WITH timing AS (SELECT clock_timestamp() AS now)
UPDATE %s
SET version = version + 1,
    expires_at = timing.now + ($4 * interval '1 microsecond')
FROM timing
WHERE lease_key = $1
  AND token = $2
  AND version = $3
  AND expires_at > timing.now
RETURNING lease_key, token, owner, version, acquired_at, expires_at`, store.table)
}

func (store *Store) lookupQuery() string {
	return fmt.Sprintf(`SELECT lease_key, token, owner, version, acquired_at, expires_at
FROM %s
WHERE lease_key = $1 AND expires_at > clock_timestamp()`, store.table)
}

func (store *Store) lookupAnyWithRemaining(ctx context.Context, key lease.Key) (lease.Lease, time.Duration, error) {
	query := fmt.Sprintf(`SELECT lease_key, token, owner, version, acquired_at, expires_at,
       GREATEST(0, CEIL(EXTRACT(EPOCH FROM (expires_at - clock_timestamp())) * 1000))::bigint
FROM %s
WHERE lease_key = $1`, store.table)
	row := store.db.QueryRowContext(ctx, query, key.String())
	var keyText, tokenText, owner string
	var version int64
	var acquiredAt, expiresAt time.Time
	var remainingMilliseconds int64
	if err := row.Scan(&keyText, &tokenText, &owner, &version, &acquiredAt, &expiresAt, &remainingMilliseconds); err != nil {
		return lease.Lease{}, 0, err
	}
	value, err := makeLease(keyText, tokenText, owner, version, acquiredAt, expiresAt)
	if err != nil {
		return lease.Lease{}, 0, err
	}
	if remainingMilliseconds < 0 {
		remainingMilliseconds = 0
	}
	return value, time.Duration(remainingMilliseconds) * time.Millisecond, nil
}

type rowScanner interface{ Scan(...any) error }

func scanLease(row rowScanner) (lease.Lease, error) {
	var keyText, tokenText, owner string
	var version int64
	var acquiredAt, expiresAt time.Time
	if err := row.Scan(&keyText, &tokenText, &owner, &version, &acquiredAt, &expiresAt); err != nil {
		return lease.Lease{}, err
	}
	return makeLease(keyText, tokenText, owner, version, acquiredAt, expiresAt)
}

func makeLease(keyText, tokenText, owner string, version int64, acquiredAt, expiresAt time.Time) (lease.Lease, error) {
	if version <= 0 {
		return lease.Lease{}, errors.New("postgres lease: invalid stored version")
	}
	key, err := lease.ParseKey(keyText)
	if err != nil {
		return lease.Lease{}, err
	}
	token, err := lease.ParseToken(tokenText)
	if err != nil {
		return lease.Lease{}, err
	}
	value := lease.Lease{
		Key:        key,
		Token:      token,
		Owner:      owner,
		Version:    uint64(version),
		AcquiredAt: acquiredAt.UTC().Round(0),
		ExpiresAt:  expiresAt.UTC().Round(0),
	}
	if err := value.Validate(); err != nil {
		return lease.Lease{}, err
	}
	return value, nil
}

func durationMicroseconds(value time.Duration) int64 {
	microseconds := value / time.Microsecond
	if value%time.Microsecond != 0 {
		microseconds++
	}
	if microseconds < 1 {
		return 1
	}
	return int64(microseconds)
}

func invalidRequest(operation string) error {
	return faults.New(faults.CodeInvalidArgument, "invalid PostgreSQL lease request", faults.WithReason("invalid_postgres_lease_request"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
}

func validTableName(value string) bool {
	if strings.TrimSpace(value) != value || value == "" {
		return false
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if len(part) == 0 || len(part) > maximumPostgresIdentifierBytes || !identifierPattern.MatchString(part) {
			return false
		}
	}
	return true
}

func indexName(table string) string {
	base := table
	if separator := strings.LastIndexByte(base, '.'); separator >= 0 {
		base = base[separator+1:]
	}
	const suffix = "_expires_at_idx"
	if len(base)+len(suffix) <= maximumPostgresIdentifierBytes {
		return base + suffix
	}
	digest := sha256.Sum256([]byte(base))
	hash := hex.EncodeToString(digest[:4])
	maximumBase := maximumPostgresIdentifierBytes - len(suffix) - len(hash) - 1
	return base[:maximumBase] + "_" + hash + suffix
}
