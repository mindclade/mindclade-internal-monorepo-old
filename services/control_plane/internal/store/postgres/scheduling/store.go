// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package schedulingpostgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"

	"go.mindclade.dev/control/scheduling"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
)

// The seam this package exists to fill. If control/scheduling widens
// Repository, this line is what fails first, in this package, rather than in
// whichever composition root happened to reference the store.
var _ scheduling.Repository = (*Store)(nil)

// executor is the subset of *sql.DB and *sql.Tx these methods use. Resolving it
// per call rather than binding one at construction is what lets a read join a
// caller's transaction instead of opening its own.
type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (store *Store) executor(ctx context.Context) executor {
	if tx, ok := transaction.FromContext(ctx); ok {
		return tx
	}
	return store.db
}

// Readiness verifies that every table and every column this adapter reads is
// queryable. It deliberately does not treat an empty reservation table or an
// unrecorded quota as unhealthy: a control plane that has admitted nothing yet
// is a new deployment, not a broken one. It does check that the singleton
// ledger row exists, because that row is not data -- it is the schema's
// initialization, and a store whose ledger row is missing cannot mint an epoch.
func (store *Store) Readiness(ctx context.Context) error {
	const operation = "scheduling.postgres.Readiness"
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	queries := []string{
		fmt.Sprintf(`SELECT reservation_id,placement_key,capacity_domain,tenant,run_id,stage_id,attempt,`+
			`state,lease_fence,sequence,created_at,expires_at,bound_at,finalized_at,preemptor_id,`+
			`resource_version,resource_generation,total_cpu,total_memory,total_ephemeral_storage,`+
			`total_gpu,total_pods,document,written_at FROM %s LIMIT 0`, store.reservations),
		fmt.Sprintf(`SELECT capacity_domain,nominal_cpu,nominal_memory,nominal_ephemeral_storage,`+
			`nominal_gpu,nominal_pods,document,written_at FROM %s LIMIT 0`, store.quotas),
		fmt.Sprintf(`SELECT tenant,weight,document,written_at FROM %s LIMIT 0`, store.weights),
		fmt.Sprintf(`SELECT singleton,fence,epoch,updated_at FROM %s LIMIT 0`, store.ledger),
	}
	for _, query := range queries {
		rows, err := store.db.QueryContext(ctx, query)
		if err != nil {
			return provider(ctx, err, operation)
		}
		if err := rows.Close(); err != nil {
			return provider(ctx, err, operation)
		}
	}
	var present bool
	err := store.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT true FROM %s WHERE singleton`, store.ledger)).Scan(&present)
	if err != nil {
		return provider(ctx, err, operation)
	}
	return nil
}

func (store *Store) validate(ctx context.Context, operation string) error {
	if ctx == nil {
		return domainError(ctx, faults.CodeInvalidArgument, "context_nil", "context is required", operation)
	}
	if store == nil || store.db == nil || nilInterface(store.clock) || store.generator == nil ||
		nilInterface(store.recorder) || nilInterface(store.messages) ||
		store.audits == nil || store.events == nil || store.retries == nil {
		return domainError(ctx, faults.CodeFailedPrecondition, "scheduling_store_unconfigured",
			"scheduling store is not configured", operation)
	}
	if err := ctx.Err(); err != nil {
		return faults.Wrap(err, faults.CodeOf(err), faults.PublicMessageOf(err),
			faults.WithReason("scheduling_context_done"), faults.WithOperation(operation),
			faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}

// runMutation is the one place a mutation reaches the database.
//
// It refuses a nested transaction rather than joining one. The contract on
// Repository is that the mutation, its audit record, and its outbox append
// share a SERIALIZABLE transaction whose ledger re-check happens inside it;
// joining an ambient transaction of unknown isolation would silently downgrade
// that guarantee, and the caller would have no way to notice.
//
// Snapshot and Held run through here too, even though the interface presents
// them as reads. Both re-seal expired holds before reading the ledger, so both
// are mutations wearing a reader's signature.
func runMutation[T any](ctx context.Context, store *Store, operation string, function func(context.Context) (T, error)) (T, error) {
	var zero T
	if err := store.validate(ctx, operation); err != nil {
		return zero, err
	}
	if _, nested := transaction.FromContext(ctx); nested {
		return zero, domainError(ctx, faults.CodeFailedPrecondition,
			"scheduling_nested_transaction_unsupported",
			"scheduling mutations require their own serializable transaction", operation)
	}
	var result T
	_, err := store.retries.Do(ctx, operation, func(attemptContext context.Context, _ retry.Attempt) error {
		var attemptErr error
		result, attemptErr = transaction.Run(attemptContext, store.db,
			transaction.Options{Isolation: sql.LevelSerializable},
			func(txContext context.Context, _ *sql.Tx) (T, error) { return function(txContext) })
		// SQLSTATE 40001 is the correct answer to honest contention, not a
		// fault: the transaction has to be replayed, and only the retry
		// executor can decide how long that is worth doing.
		//
		// Both surfaces are re-wrapped, which orchestration did not need to do.
		// There, a serialization failure was almost always detected at COMMIT,
		// which storage/sql/transaction reports as sql_transaction_failed. Here
		// the singleton ledger lock is taken as the first statement of every
		// mutation, so the loser usually learns at that statement instead, and
		// storage/sql/postgres reports that as postgres_transaction_retryable
		// with its own five-attempt ceiling. Leaving that path alone would
		// silently cap this store's budget at five wherever it matters most.
		if retryableSerialization(attemptErr) {
			return faults.Wrap(attemptErr, faults.CodeAborted, "serializable scheduling transaction must be retried",
				faults.WithReason("scheduling_serialization_retry"), faults.WithOperation(operation),
				faults.WithRetryPolicy(faults.BackoffRetry(schedulingMutationMaxAttempts)),
				faults.WithContextMetadata(attemptContext))
		}
		return attemptErr
	})
	return result, err
}

func retryableSerialization(err error) bool {
	if !faults.IsCode(err, faults.CodeAborted) {
		return false
	}
	return faults.IsReason(err, "sql_transaction_failed") ||
		faults.IsReason(err, "postgres_transaction_retryable")
}

func marshalDocument(ctx context.Context, value any, operation string) ([]byte, error) {
	document, err := json.Marshal(value)
	if err != nil {
		return nil, internal(ctx, err, operation, "scheduling_document_encoding_failed")
	}
	return document, nil
}

// decodeDocument revalidates every stored record on the way out. A row edited
// out of band is then unusable rather than authoritative, which is the same
// property the domain's own seal checks give an in-memory value -- and for a
// reservation it is load-bearing, because Reservation.Validate re-derives the
// digest that seals the record from its fields.
func decodeDocument[T interface{ Validate() error }](ctx context.Context, document []byte, operation string) (T, error) {
	var value T
	if err := json.Unmarshal(document, &value); err != nil {
		return value, internal(ctx, err, operation, "scheduling_document_decoding_failed")
	}
	if err := value.Validate(); err != nil {
		return value, internal(ctx, err, operation, "scheduling_document_invalid")
	}
	return value, nil
}

// sqlUint refuses a uint64 that PostgreSQL's signed bigint cannot hold. Letting
// it wrap would store a negative fence, epoch, or resource amount that every
// later comparison would read backwards.
func sqlUint(ctx context.Context, value uint64, label, operation string) (int64, error) {
	if value > math.MaxInt64 {
		return 0, domainError(ctx, faults.CodeInvalidArgument,
			"scheduling_"+label+"_out_of_range", label+" is outside PostgreSQL bounds", operation)
	}
	return int64(value), nil
}

// reservationDocument adapts Reservation to decodeDocument's constraint.
type reservationDocument scheduling.Reservation

func (document reservationDocument) Validate() error {
	return scheduling.Reservation(document).Validate()
}
