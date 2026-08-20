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

	"go.mindclade.dev/libs/go/audit"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
)

const DefaultTable = "mindclade_audit_events"

type Option func(*Store) error

func WithTable(value string) Option {
	return func(store *Store) error {
		identifier, err := sqlpostgres.QualifiedIdentifier(value)
		if err != nil {
			return err
		}
		store.table = identifier
		return nil
	}
}

type databaseExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Store implements audit.Recorder and automatically joins a transaction
// installed by storage/sql/transaction. This permits the domain mutation,
// audit record, idempotency completion, and outbox insert to commit atomically.
type Store struct {
	db    *sql.DB
	table string
}

var _ audit.Recorder = (*Store)(nil)

func New(db *sql.DB, options ...Option) (*Store, error) {
	if db == nil {
		return nil, faults.New(faults.CodeInvalidArgument, "database must not be nil", faults.WithReason("nil_database"), faults.WithOperation("audit.postgres.New"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	store := &Store{db: db, table: DefaultTable}
	for _, option := range options {
		if option != nil {
			if err := option(store); err != nil {
				return nil, err
			}
		}
	}
	if !sqlpostgres.ValidQualifiedIdentifier(store.table) {
		return nil, faults.New(faults.CodeInvalidArgument, "invalid audit table", faults.WithReason("invalid_audit_table"), faults.WithOperation("audit.postgres.New"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return store, nil
}

func DDL(table string) (string, error) {
	identifier, err := sqlpostgres.QualifiedIdentifier(table)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    event_id TEXT PRIMARY KEY,
    event_digest TEXT NOT NULL,
    schema_version INTEGER NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    action TEXT NOT NULL,
    outcome TEXT NOT NULL,
    tenant_id TEXT NULL,
    organization_id TEXT NULL,
    actor_kind TEXT NOT NULL,
    actor_subject TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NULL,
    request_id TEXT NULL,
    event_json JSONB NOT NULL,
    inserted_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX IF NOT EXISTS mindclade_audit_events_occurred_at_idx ON %s (occurred_at DESC);
CREATE INDEX IF NOT EXISTS mindclade_audit_events_tenant_idx ON %s (tenant_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS mindclade_audit_events_target_idx ON %s (target_type, target_id, occurred_at DESC);`, identifier, identifier, identifier, identifier), nil
}

func (store *Store) Record(ctx context.Context, event audit.Event) error {
	const operation = "audit.postgres.Record"
	if ctx == nil {
		return faults.Wrap(audit.ErrNilContext, faults.CodeInvalidArgument, "audit context must not be nil", faults.WithReason("nil_context"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if store == nil || store.db == nil || !sqlpostgres.ValidQualifiedIdentifier(store.table) {
		return faults.Wrap(audit.ErrNilRecorder, faults.CodeFailedPrecondition, "audit PostgreSQL store is not configured", faults.WithReason("audit_store_not_configured"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := event.Validate(); err != nil {
		return err
	}
	payload, err := event.MarshalJSON()
	if err != nil {
		return faults.Wrap(err, faults.CodeInternal, "audit event serialization failed", faults.WithReason("audit_serialization_failed"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	digest := identifiers.SHA256(payload)
	actor := event.Actor()
	target := event.Target()
	metadata, _ := event.RequestMetadata()

	executor := databaseExecutor(store.db)
	if tx, ok := transaction.FromContext(ctx); ok {
		executor = tx
	}
	query := fmt.Sprintf(`INSERT INTO %s (
event_id,event_digest,schema_version,occurred_at,action,outcome,tenant_id,
organization_id,actor_kind,actor_subject,target_type,target_id,request_id,event_json
) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),$9,$10,$11,NULLIF($12,''),NULLIF($13,''),$14)
ON CONFLICT (event_id) DO NOTHING`, store.table)
	result, err := executor.ExecContext(ctx, query,
		event.ID().String(), digest.String(), event.SchemaVersion(), event.OccurredAt(), event.Action().String(), event.Outcome().String(),
		actor.TenantID().String(), actor.OrganizationID().String(), actor.Kind().String(), actor.Subject(), target.Type(), target.ID().String(), metadata.RequestID.String(), payload,
	)
	if err != nil {
		return qualify(ctx, err, operation)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return qualify(ctx, err, operation+".RowsAffected")
	}
	if affected == 1 {
		return nil
	}

	var existing string
	lookup := fmt.Sprintf(`SELECT event_digest FROM %s WHERE event_id=$1`, store.table)
	if err := executor.QueryRowContext(ctx, lookup, event.ID().String()).Scan(&existing); err != nil {
		return qualify(ctx, err, operation+".LookupConflict")
	}
	if existing == digest.String() {
		return nil
	}
	return faults.Wrap(errors.Join(audit.ErrRecorderFailure, ErrConflict), faults.CodeConflict, "audit event identifier already exists with different content", faults.WithReason("audit_event_conflict"), faults.WithOperation(operation), faults.WithField("audit_event_id", event.ID().String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

func qualify(ctx context.Context, err error, operation string) error {
	qualified := sqlpostgres.Qualify(ctx, err, operation)
	return faults.Wrap(errors.Join(audit.ErrRecorderFailure, qualified), faults.CodeOf(qualified), "audit event could not be recorded", faults.WithReason(valueOr(faults.ReasonOf(qualified), "audit_postgres_failed")), faults.WithOperation(operation), faults.WithFields(faults.FieldsOf(qualified)), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.RetryPolicyOf(qualified)))
}
func valueOr(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
