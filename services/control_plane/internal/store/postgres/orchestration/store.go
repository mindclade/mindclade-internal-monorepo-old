// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestrationpostgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
)

// The seam this package exists to fill. If control/orchestration widens
// Repository, this line is what fails first, in this package, rather than in
// whichever composition root happened to reference the store.
var _ orchestration.Repository = (*Store)(nil)

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
// queryable. It deliberately does not treat an empty workflow catalog as
// unhealthy: a control plane that has published no workflow yet is a new
// deployment, not a broken one.
func (store *Store) Readiness(ctx context.Context) error {
	const operation = "orchestration.postgres.Readiness"
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	queries := []string{
		fmt.Sprintf(`SELECT workflow_id,name,definition_digest,schema_version,stage_count,document,written_at FROM %s LIMIT 0`, store.workflows),
		fmt.Sprintf(`SELECT run_id,stage_id,job_id,state,attempts,updated_at,resource_version,resource_generation,document,written_at FROM %s LIMIT 0`, store.stages),
		fmt.Sprintf(`SELECT run_id,stage_id,attempt,job_id,state,fencing_token,execution_ticket_id,sequence,created_at,updated_at,resource_version,resource_generation,document,written_at FROM %s LIMIT 0`, store.attempts),
		fmt.Sprintf(`SELECT idempotency_scope,idempotency_key,run_id,stage_id,origin,requested_at,document,written_at FROM %s LIMIT 0`, store.cancellations),
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
	return nil
}

func (store *Store) validate(ctx context.Context, operation string) error {
	if ctx == nil {
		return domainError(ctx, faults.CodeInvalidArgument, "orchestration_nil_context", "context is required", operation)
	}
	if store == nil || store.db == nil || nilInterface(store.clock) || store.generator == nil ||
		nilInterface(store.recorder) || nilInterface(store.messages) ||
		store.audits == nil || store.events == nil || store.retries == nil {
		return domainError(ctx, faults.CodeFailedPrecondition, "orchestration_store_unconfigured",
			"orchestration store is not configured", operation)
	}
	if err := ctx.Err(); err != nil {
		return faults.Wrap(err, faults.CodeOf(err), faults.PublicMessageOf(err),
			faults.WithReason("orchestration_context_done"), faults.WithOperation(operation),
			faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}

// runMutation is the one place a mutation reaches the database.
//
// It refuses a nested transaction rather than joining one. The contract on
// Repository is that the mutation, its audit record, and its outbox append
// share a SERIALIZABLE transaction whose precondition is read inside it;
// joining an ambient transaction of unknown isolation would silently downgrade
// that guarantee, and the caller would have no way to notice.
func runMutation[T any](ctx context.Context, store *Store, operation string, function func(context.Context) (T, error)) (T, error) {
	var zero T
	if err := store.validate(ctx, operation); err != nil {
		return zero, err
	}
	if _, nested := transaction.FromContext(ctx); nested {
		return zero, domainError(ctx, faults.CodeFailedPrecondition,
			"orchestration_nested_transaction_unsupported",
			"orchestration mutations require their own serializable transaction", operation)
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
		if faults.IsCode(attemptErr, faults.CodeAborted) && faults.IsReason(attemptErr, "sql_transaction_failed") {
			return faults.Wrap(attemptErr, faults.CodeAborted, "serializable orchestration transaction must be retried",
				faults.WithReason("orchestration_serialization_retry"), faults.WithOperation(operation),
				faults.WithRetryPolicy(faults.BackoffRetry(orchestrationMutationMaxAttempts)),
				faults.WithContextMetadata(attemptContext))
		}
		return attemptErr
	})
	return result, err
}

func marshalDocument(ctx context.Context, value any, operation string) ([]byte, error) {
	document, err := json.Marshal(value)
	if err != nil {
		return nil, internal(ctx, err, operation, "orchestration_document_encoding_failed")
	}
	return document, nil
}

// decodeDocument revalidates every stored record on the way out. A row edited
// out of band is then unusable rather than authoritative, which is the same
// property the domain's own seal checks give an in-memory value.
func decodeDocument[T interface{ Validate() error }](ctx context.Context, document []byte, operation string) (T, error) {
	var value T
	if err := json.Unmarshal(document, &value); err != nil {
		return value, internal(ctx, err, operation, "orchestration_document_decoding_failed")
	}
	if err := value.Validate(); err != nil {
		return value, internal(ctx, err, operation, "orchestration_document_invalid")
	}
	return value, nil
}

// sqlUint refuses a uint64 that PostgreSQL's signed bigint cannot hold. Letting
// it wrap would store a negative fence or generation that every later
// comparison would read backwards.
func sqlUint(ctx context.Context, value uint64, label, operation string) (int64, error) {
	if value > math.MaxInt64 {
		return 0, domainError(ctx, faults.CodeInvalidArgument,
			"orchestration_"+label+"_out_of_range", label+" is outside PostgreSQL bounds", operation)
	}
	return int64(value), nil
}

// canonicalJoin and stageDigest restate control/orchestration's stage seal.
//
// TransitionStage has to mint the same resourceversion.Version the reference
// adapter would mint, because a record written by one adapter is read and
// transitioned by the other. The domain computes that digest in an unexported
// function, so the definition is repeated here rather than approximated. The
// unit separator cannot appear in any validated field, so no two distinct field
// lists collide on one preimage.
//
// TestTransitionStageMatchesMemoryRepositorySeal drives the same transition
// through orchestration.MemoryRepository and this store and compares versions,
// so a change to either definition fails a test instead of forking the format.
func canonicalJoin(values ...string) string { return strings.Join(values, "\x1f") }

func stageDigest(record orchestration.StageRecord) identifiers.Digest {
	return identifiers.SHA256String(canonicalJoin(
		record.RunID,
		record.JobID,
		record.StageID,
		string(record.State),
		strconv.FormatUint(uint64(record.Attempts), 10),
		strconv.FormatInt(record.UpdatedAt.UnixNano(), 10),
	))
}
