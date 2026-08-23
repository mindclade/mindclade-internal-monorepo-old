// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestrationpostgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/libs/go/faults"
)

type attemptResult struct {
	record   orchestration.AttemptRecord
	replayed bool
}

type cancellationResult struct {
	intent   orchestration.CancellationIntent
	replayed bool
}

// PutAttempt writes an attempt under a monotonic version precondition.
//
// Three cases, and the middle one is why this is not an upsert. An identical
// version is a replay of a write that already landed. A version at or behind the
// stored one is a stale writer -- typically a worker that lost its lease and has
// not noticed -- and is refused. Only a strictly newer generation overwrites,
// which is what stops a late status from resurrecting state a replacement has
// already moved past.
func (store *Store) PutAttempt(ctx context.Context, record orchestration.AttemptRecord) (orchestration.AttemptRecord, bool, error) {
	const operation = "orchestration.postgres.PutAttempt"
	if err := store.validate(ctx, operation); err != nil {
		return orchestration.AttemptRecord{}, false, err
	}
	if err := record.Validate(); err != nil {
		return orchestration.AttemptRecord{}, false, err
	}
	document, err := marshalDocument(ctx, record, operation)
	if err != nil {
		return orchestration.AttemptRecord{}, false, err
	}
	attempt, err := sqlUint(ctx, uint64(record.Attempt), "attempt", operation)
	if err != nil {
		return orchestration.AttemptRecord{}, false, err
	}
	fencingToken, err := sqlUint(ctx, record.FencingToken, "fencing_token", operation)
	if err != nil {
		return orchestration.AttemptRecord{}, false, err
	}
	sequence, err := sqlUint(ctx, record.Sequence, "sequence", operation)
	if err != nil {
		return orchestration.AttemptRecord{}, false, err
	}
	generation, err := sqlUint(ctx, record.Version.Generation(), "resource_generation", operation)
	if err != nil {
		return orchestration.AttemptRecord{}, false, err
	}
	result, err := runMutation(ctx, store, operation, func(txContext context.Context) (attemptResult, error) {
		existing, found, lookupErr := store.lockAttempt(txContext, record.RunID, record.StageID, record.Attempt, operation)
		if lookupErr != nil {
			return attemptResult{}, lookupErr
		}
		if found {
			if existing.Version.String() == record.Version.String() {
				return attemptResult{record: existing, replayed: true}, nil
			}
			if existing.Version.Generation() >= record.Version.Generation() {
				return attemptResult{}, domainError(txContext, faults.CodeConflict,
					"attempt_version_stale", "attempt resource version is stale", operation)
			}
			query := fmt.Sprintf(`UPDATE %s SET
state=$4,fencing_token=$5,execution_ticket_id=$6,sequence=$7,updated_at=$8,
resource_version=$9,resource_generation=$10,document=$11::jsonb,written_at=$8
WHERE run_id=$1 AND stage_id=$2 AND attempt=$3`, store.attempts)
			if _, execErr := store.executor(txContext).ExecContext(txContext, query,
				record.RunID, record.StageID, attempt,
				string(record.State), fencingToken, record.ExecutionTicketID, sequence,
				record.UpdatedAt.Round(0).UTC(), record.Version.String(), generation, document,
			); execErr != nil {
				return attemptResult{}, provider(txContext, execErr, operation)
			}
		} else {
			query := fmt.Sprintf(`INSERT INTO %s (
run_id,stage_id,attempt,job_id,state,fencing_token,execution_ticket_id,sequence,
created_at,updated_at,resource_version,resource_generation,document,written_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$10)`, store.attempts)
			if _, execErr := store.executor(txContext).ExecContext(txContext, query,
				record.RunID, record.StageID, attempt, record.JobID,
				string(record.State), fencingToken, record.ExecutionTicketID, sequence,
				record.CreatedAt.Round(0).UTC(), record.UpdatedAt.Round(0).UTC(),
				record.Version.String(), generation, document,
			); execErr != nil {
				return attemptResult{}, provider(txContext, execErr, operation)
			}
		}
		if emitErr := store.emitAttempt(txContext, "orchestration.attempt."+string(record.State), record); emitErr != nil {
			return attemptResult{}, emitErr
		}
		return attemptResult{record: record}, nil
	})
	if err != nil {
		return orchestration.AttemptRecord{}, false, err
	}
	return result.record, result.replayed, nil
}

func (store *Store) GetAttempt(ctx context.Context, runID, stageID string, attempt uint32) (orchestration.AttemptRecord, error) {
	const operation = "orchestration.postgres.GetAttempt"
	if err := store.validate(ctx, operation); err != nil {
		return orchestration.AttemptRecord{}, err
	}
	number, err := sqlUint(ctx, uint64(attempt), "attempt", operation)
	if err != nil {
		return orchestration.AttemptRecord{}, err
	}
	var document []byte
	err = store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE run_id=$1 AND stage_id=$2 AND attempt=$3`, store.attempts),
		runID, stageID, number).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return orchestration.AttemptRecord{}, domainError(ctx, faults.CodeNotFound,
			"attempt_not_found", "attempt was not found", operation)
	}
	if err != nil {
		return orchestration.AttemptRecord{}, provider(ctx, err, operation)
	}
	decoded, err := decodeDocument[attemptDocument](ctx, document, operation)
	if err != nil {
		return orchestration.AttemptRecord{}, err
	}
	return orchestration.AttemptRecord(decoded), nil
}

// LatestAttempt returns the highest-numbered attempt for a stage.
//
// Ordered by attempt rather than by time: attempt numbers are the domain's
// ordering and are allocated before any timestamp is written, so two attempts
// created inside one clock tick still order correctly.
func (store *Store) LatestAttempt(ctx context.Context, runID, stageID string) (orchestration.AttemptRecord, error) {
	const operation = "orchestration.postgres.LatestAttempt"
	if err := store.validate(ctx, operation); err != nil {
		return orchestration.AttemptRecord{}, err
	}
	var document []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE run_id=$1 AND stage_id=$2 ORDER BY attempt DESC LIMIT 1`, store.attempts),
		runID, stageID).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return orchestration.AttemptRecord{}, domainError(ctx, faults.CodeNotFound,
			"attempt_not_found", "no attempt was found for the stage", operation)
	}
	if err != nil {
		return orchestration.AttemptRecord{}, provider(ctx, err, operation)
	}
	decoded, err := decodeDocument[attemptDocument](ctx, document, operation)
	if err != nil {
		return orchestration.AttemptRecord{}, err
	}
	return orchestration.AttemptRecord(decoded), nil
}

// RecordCancellation stores the intent, keyed by idempotency identity.
//
// That identity is the primary key, so a retried cancel request replays. Reusing
// one key for a different run or stage is refused rather than absorbed: it is a
// caller bug, and returning the first intent would report success for a
// cancellation that never happened.
func (store *Store) RecordCancellation(ctx context.Context, intent orchestration.CancellationIntent) (orchestration.CancellationIntent, bool, error) {
	const operation = "orchestration.postgres.RecordCancellation"
	if err := store.validate(ctx, operation); err != nil {
		return orchestration.CancellationIntent{}, false, err
	}
	if err := intent.Validate(); err != nil {
		return orchestration.CancellationIntent{}, false, err
	}
	document, err := marshalDocument(ctx, intent, operation)
	if err != nil {
		return orchestration.CancellationIntent{}, false, err
	}
	scope := intent.Idempotency.Scope.String()
	key := intent.Idempotency.Key.String()
	// A run-scoped cancellation names no stage, and the table's CHECK ties the
	// two together, so the column has to be NULL rather than an empty string.
	var stage any
	if intent.StageID != "" {
		stage = intent.StageID
	}
	requestedAt := intent.RequestedAt.Round(0).UTC()
	result, err := runMutation(ctx, store, operation, func(txContext context.Context) (cancellationResult, error) {
		existing, found, lookupErr := store.lockCancellation(txContext, scope, key, operation)
		if lookupErr != nil {
			return cancellationResult{}, lookupErr
		}
		if found {
			if existing.RunID != intent.RunID || existing.StageID != intent.StageID {
				return cancellationResult{}, domainError(txContext, faults.CodeConflict,
					"cancellation_idempotency_mismatch",
					"the idempotency key was reused for a different cancellation", operation)
			}
			return cancellationResult{intent: existing, replayed: true}, nil
		}
		query := fmt.Sprintf(`INSERT INTO %s (
idempotency_scope,idempotency_key,run_id,stage_id,origin,requested_at,document,written_at
) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$6)`, store.cancellations)
		if _, execErr := store.executor(txContext).ExecContext(txContext, query,
			scope, key, intent.RunID, stage, string(intent.Origin), requestedAt, document,
		); execErr != nil {
			return cancellationResult{}, provider(txContext, execErr, operation)
		}
		if emitErr := store.emitCancellation(txContext, intent); emitErr != nil {
			return cancellationResult{}, emitErr
		}
		return cancellationResult{intent: intent}, nil
	})
	if err != nil {
		return orchestration.CancellationIntent{}, false, err
	}
	return result.intent, result.replayed, nil
}

func (store *Store) lockAttempt(ctx context.Context, runID, stageID string, attempt uint32, operation string) (orchestration.AttemptRecord, bool, error) {
	number, err := sqlUint(ctx, uint64(attempt), "attempt", operation)
	if err != nil {
		return orchestration.AttemptRecord{}, false, err
	}
	var document []byte
	err = store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE run_id=$1 AND stage_id=$2 AND attempt=$3 FOR UPDATE`, store.attempts),
		runID, stageID, number).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return orchestration.AttemptRecord{}, false, nil
	}
	if err != nil {
		return orchestration.AttemptRecord{}, false, provider(ctx, err, operation+".Lock")
	}
	decoded, decodeErr := decodeDocument[attemptDocument](ctx, document, operation)
	if decodeErr != nil {
		return orchestration.AttemptRecord{}, false, decodeErr
	}
	return orchestration.AttemptRecord(decoded), true, nil
}

func (store *Store) lockCancellation(ctx context.Context, scope, key, operation string) (orchestration.CancellationIntent, bool, error) {
	var document []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE idempotency_scope=$1 AND idempotency_key=$2 FOR UPDATE`, store.cancellations),
		scope, key).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return orchestration.CancellationIntent{}, false, nil
	}
	if err != nil {
		return orchestration.CancellationIntent{}, false, provider(ctx, err, operation+".Lock")
	}
	decoded, decodeErr := decodeDocument[cancellationDocument](ctx, document, operation)
	if decodeErr != nil {
		return orchestration.CancellationIntent{}, false, decodeErr
	}
	return orchestration.CancellationIntent(decoded), true, nil
}

// attemptDocument adapts AttemptRecord to decodeDocument's constraint.
type attemptDocument orchestration.AttemptRecord

func (document attemptDocument) Validate() error {
	return orchestration.AttemptRecord(document).Validate()
}

// cancellationDocument adapts CancellationIntent to decodeDocument's constraint.
type cancellationDocument orchestration.CancellationIntent

func (document cancellationDocument) Validate() error {
	return orchestration.CancellationIntent(document).Validate()
}
