// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"go.mindclade.dev/libs/go/coordination/cursor"
	"go.mindclade.dev/libs/go/faults"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
)

type Store struct {
	db    *sql.DB
	table string
}

func New(db *sql.DB, table string) (*Store, error) {
	if db == nil {
		return nil, invalid(nil, "cursor.postgres.New")
	}
	value, err := sqlpostgres.QualifiedIdentifier(table)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, table: value}, nil
}
func DDL(table string) (string, error) {
	value, err := sqlpostgres.QualifiedIdentifier(table)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
namespace TEXT NOT NULL,
name TEXT NOT NULL,
sequence BIGINT NOT NULL CHECK (sequence >= 0),
opaque BYTEA NOT NULL DEFAULT ''::bytea,
fence BIGINT NOT NULL CHECK (fence > 0),
version BIGINT NOT NULL CHECK (version > 0),
updated_at TIMESTAMPTZ NOT NULL,
PRIMARY KEY(namespace,name)
);`, value), nil
}

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (store *Store) exec(ctx context.Context) executor {
	if tx, ok := transaction.FromContext(ctx); ok {
		return tx
	}
	return store.db
}
func (store *Store) Load(ctx context.Context, key cursor.Key) (cursor.Cursor, error) {
	if ctx == nil || store == nil || store.db == nil || key.Validate() != nil {
		return cursor.Cursor{}, invalid(ctx, "cursor.postgres.Load")
	}
	query := fmt.Sprintf(`SELECT sequence,opaque,fence,version,updated_at FROM %s WHERE namespace=$1 AND name=$2`, store.table)
	var sequence, fence, version uint64
	var opaque []byte
	var updatedAt sql.NullTime
	err := store.exec(ctx).QueryRowContext(ctx, query, key.Namespace, key.Name).Scan(&sequence, &opaque, &fence, &version, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return cursor.Cursor{}, notFound(ctx, key, "cursor.postgres.Load")
	}
	if err != nil {
		return cursor.Cursor{}, provider(ctx, err, "cursor.postgres.Load")
	}
	if !updatedAt.Valid {
		return cursor.Cursor{}, faults.Wrap(cursor.ErrInvalidRequest, faults.CodeInternal, "stored cursor has no timestamp", faults.WithReason("invalid_stored_cursor"), faults.WithOperation("cursor.postgres.Load"))
	}
	value, err := cursor.New(key, sequence, opaque, fence, version, updatedAt.Time)
	if err != nil {
		return cursor.Cursor{}, faults.Wrap(err, faults.CodeInternal, "stored cursor is invalid", faults.WithReason("invalid_stored_cursor"), faults.WithOperation("cursor.postgres.Load"))
	}
	return value, nil
}
func (store *Store) Advance(ctx context.Context, request cursor.AdvanceRequest) (cursor.Cursor, error) {
	if ctx == nil || store == nil || store.db == nil {
		return cursor.Cursor{}, invalid(ctx, "cursor.postgres.Advance")
	}
	if err := request.Validate(); err != nil {
		return cursor.Cursor{}, err
	}
	exec := store.exec(ctx)
	if request.ExpectedVersion == 0 {
		query := fmt.Sprintf(`INSERT INTO %s(namespace,name,sequence,opaque,fence,version,updated_at) VALUES($1,$2,$3,$4,$5,1,$6) ON CONFLICT DO NOTHING`, store.table)
		result, err := exec.ExecContext(ctx, query, request.Key.Namespace, request.Key.Name, request.Sequence, request.Opaque, request.Fence, request.UpdatedAt)
		if err != nil {
			return cursor.Cursor{}, provider(ctx, err, "cursor.postgres.Advance.insert")
		}
		affected, _ := result.RowsAffected()
		if affected != 1 {
			return cursor.Cursor{}, conflict(ctx, request.Key, "cursor.postgres.Advance.insert")
		}
		return cursor.New(request.Key, request.Sequence, request.Opaque, request.Fence, 1, request.UpdatedAt)
	}
	query := fmt.Sprintf(`UPDATE %s SET sequence=$1,opaque=$2,fence=$3,version=version+1,updated_at=$4 WHERE namespace=$5 AND name=$6 AND version=$7 AND fence <= $3 AND sequence <= $1`, store.table)
	result, err := exec.ExecContext(ctx, query, request.Sequence, request.Opaque, request.Fence, request.UpdatedAt, request.Key.Namespace, request.Key.Name, request.ExpectedVersion)
	if err != nil {
		return cursor.Cursor{}, provider(ctx, err, "cursor.postgres.Advance.update")
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		current, loadErr := store.Load(ctx, request.Key)
		if loadErr == nil {
			if request.Fence < current.Fence {
				return cursor.Cursor{}, stale(ctx, request.Key)
			}
			if request.Sequence < current.Sequence {
				return cursor.Cursor{}, regression(ctx, request.Key)
			}
		}
		return cursor.Cursor{}, conflict(ctx, request.Key, "cursor.postgres.Advance.update")
	}
	return cursor.New(request.Key, request.Sequence, request.Opaque, request.Fence, request.ExpectedVersion+1, request.UpdatedAt)
}
func (store *Store) Delete(ctx context.Context, key cursor.Key, expected uint64) error {
	if ctx == nil || store == nil || store.db == nil || key.Validate() != nil || expected == 0 {
		return invalid(ctx, "cursor.postgres.Delete")
	}
	query := fmt.Sprintf(`DELETE FROM %s WHERE namespace=$1 AND name=$2 AND version=$3`, store.table)
	result, err := store.exec(ctx).ExecContext(ctx, query, key.Namespace, key.Name, expected)
	if err != nil {
		return provider(ctx, err, "cursor.postgres.Delete")
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return conflict(ctx, key, "cursor.postgres.Delete")
	}
	return nil
}
func invalid(ctx context.Context, op string) error {
	return faults.Wrap(cursor.ErrInvalidRequest, faults.CodeInvalidArgument, "invalid PostgreSQL cursor request", faults.WithReason("invalid_cursor_request"), faults.WithOperation(op), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
func provider(ctx context.Context, err error, op string) error {
	return faults.Wrap(sqlpostgres.Qualify(ctx, err, op), faults.CodeUnavailable, "PostgreSQL cursor operation failed", faults.WithReason("cursor_store_failed"), faults.WithOperation(op), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.BackoffRetry(0)))
}
func notFound(ctx context.Context, key cursor.Key, op string) error {
	return faults.Wrap(cursor.ErrNotFound, faults.CodeNotFound, "cursor not found", faults.WithReason("cursor_not_found"), faults.WithOperation(op), faults.WithField("cursor", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
func conflict(ctx context.Context, key cursor.Key, op string) error {
	return faults.Wrap(cursor.ErrConflict, faults.CodeAborted, "cursor compare-and-swap conflict", faults.WithReason("cursor_conflict"), faults.WithOperation(op), faults.WithField("cursor", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.ImmediateRetry(3)))
}
func stale(ctx context.Context, key cursor.Key) error {
	return faults.Wrap(cursor.ErrStaleFence, faults.CodeAborted, "cursor fencing token is stale", faults.WithReason("cursor_stale_fence"), faults.WithOperation("cursor.postgres.Advance"), faults.WithField("cursor", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
func regression(ctx context.Context, key cursor.Key) error {
	return faults.Wrap(cursor.ErrRegression, faults.CodeFailedPrecondition, "cursor sequence cannot regress", faults.WithReason("cursor_regression"), faults.WithOperation("cursor.postgres.Advance"), faults.WithField("cursor", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
