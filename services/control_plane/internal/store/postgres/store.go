// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"database/sql"

	"go.mindclade.dev/libs/go/faults"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
)

// executor is the subset of *sql.DB and *sql.Tx these adapters use. Resolving
// it per call, rather than binding one at construction, is what makes a store
// join a caller's transaction instead of opening its own.
type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// executor returns the transaction installed on ctx, or the pool. No method
// here calls transaction.Run: these repositories are always one statement, and
// a repository that opened its own transaction could not be composed into the
// mutation-and-publication commit that spans the domain write, the audit
// record, and the outbox insert.
func (store *Store) executor(ctx context.Context) executor {
	if tx, ok := transaction.FromContext(ctx); ok {
		return tx
	}
	return store.db
}

func (store *Store) validate(ctx context.Context, operation string) error {
	if ctx == nil {
		return faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, "context must not be nil",
			faults.WithReason("registry_nil_context"),
			faults.WithOperation(operation),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if store == nil || store.db == nil || nilInterface(store.clock) ||
		!validQualifiedIdentifier(store.descriptors) ||
		!validQualifiedIdentifier(store.releases) ||
		!validQualifiedIdentifier(store.graphs) {
		return faults.Wrap(ErrInvalidConfig, faults.CodeFailedPrecondition,
			"registry PostgreSQL store is not configured",
			faults.WithReason("registry_store_unconfigured"),
			faults.WithOperation(operation),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if err := ctx.Err(); err != nil {
		return faults.Wrap(err, faults.CodeOf(err), faults.PublicMessageOf(err),
			faults.WithReason("registry_context_done"),
			faults.WithOperation(operation),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return nil
}

// provider classifies a driver error through the shared PostgreSQL qualifier,
// so retryability is decided by the one place that knows the SQLSTATE codes
// rather than per adapter.
func provider(ctx context.Context, err error, operation string) error {
	qualified := sqlpostgres.Qualify(ctx, err, operation)
	return faults.Wrap(qualified, faults.CodeOf(qualified), "registry store operation failed",
		faults.WithReason("registry_store_failed"),
		faults.WithOperation(operation),
		faults.WithFields(faults.FieldsOf(qualified)),
		faults.WithRetryPolicy(faults.RetryPolicyOf(qualified)),
		faults.WithContextMetadata(ctx),
	)
}

// internal reports a broken invariant -- an encoding that should not fail, a
// stored document that will not decode. These are never retryable: the same
// input would fail the same way.
func internal(ctx context.Context, err error, operation, reason string) error {
	return faults.Wrap(err, faults.CodeInternal, "registry store invariant failed",
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithContextMetadata(ctx),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
