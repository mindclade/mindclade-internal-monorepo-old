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
	"strings"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/resourceversion"
)

// MaximumStageFetch bounds one GetStages request. The domain derives the id list
// from a graph that is itself capped, so this only ever rejects a caller that
// built the list some other way -- which is exactly when an unbounded IN clause
// would otherwise reach the database.
const MaximumStageFetch = 4096

type stageResult struct {
	record   orchestration.StageRecord
	replayed bool
}

// PutStage materializes a stage row.
//
// Insert-if-absent rather than upsert: a stage that already exists carries state
// this call does not know about, and overwriting it would silently reset a
// running stage to blocked. The existing row is returned as a replay instead,
// which is what makes StartRun safe to re-drive after a crash.
func (store *Store) PutStage(ctx context.Context, record orchestration.StageRecord) (orchestration.StageRecord, bool, error) {
	const operation = "orchestration.postgres.PutStage"
	if err := store.validate(ctx, operation); err != nil {
		return orchestration.StageRecord{}, false, err
	}
	if err := record.Validate(); err != nil {
		return orchestration.StageRecord{}, false, err
	}
	document, err := marshalDocument(ctx, record, operation)
	if err != nil {
		return orchestration.StageRecord{}, false, err
	}
	attempts, err := sqlUint(ctx, uint64(record.Attempts), "attempts", operation)
	if err != nil {
		return orchestration.StageRecord{}, false, err
	}
	written := record.UpdatedAt.Round(0).UTC()
	result, err := runMutation(ctx, store, operation, func(txContext context.Context) (stageResult, error) {
		existing, found, lookupErr := store.lockStage(txContext, record.RunID, record.StageID, operation)
		if lookupErr != nil {
			return stageResult{}, lookupErr
		}
		if found {
			return stageResult{record: existing, replayed: true}, nil
		}
		version, generation := stageVersionColumns(record.Version)
		query := fmt.Sprintf(`INSERT INTO %s (
run_id,stage_id,job_id,state,attempts,updated_at,resource_version,resource_generation,document,written_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10)`, store.stages)
		if _, execErr := store.executor(txContext).ExecContext(txContext, query,
			record.RunID, record.StageID, record.JobID, string(record.State),
			attempts, record.UpdatedAt.Round(0).UTC(), version, generation, document, written,
		); execErr != nil {
			return stageResult{}, provider(txContext, execErr, operation)
		}
		if emitErr := store.emitStage(txContext, "orchestration.stage.created", record); emitErr != nil {
			return stageResult{}, emitErr
		}
		return stageResult{record: record}, nil
	})
	if err != nil {
		return orchestration.StageRecord{}, false, err
	}
	return result.record, result.replayed, nil
}

func (store *Store) GetStage(ctx context.Context, runID, stageID string) (orchestration.StageRecord, error) {
	const operation = "orchestration.postgres.GetStage"
	if err := store.validate(ctx, operation); err != nil {
		return orchestration.StageRecord{}, err
	}
	var document []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE run_id=$1 AND stage_id=$2`, store.stages),
		runID, stageID).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return orchestration.StageRecord{}, domainError(ctx, faults.CodeNotFound,
			"stage_not_found", "stage was not found", operation)
	}
	if err != nil {
		return orchestration.StageRecord{}, provider(ctx, err, operation)
	}
	decoded, err := decodeDocument[stageDocument](ctx, document, operation)
	if err != nil {
		return orchestration.StageRecord{}, err
	}
	return orchestration.StageRecord(decoded), nil
}

// ListStages returns every stage of a run, in graph-stable order.
//
// The order is (stage_id) rather than insertion order so two reads of an
// unchanged run agree. A caller that only needs one completion's neighbourhood
// should use GetStages: this scans the run.
func (store *Store) ListStages(ctx context.Context, runID string) ([]orchestration.StageRecord, error) {
	const operation = "orchestration.postgres.ListStages"
	if err := store.validate(ctx, operation); err != nil {
		return nil, err
	}
	query := fmt.Sprintf(
		`SELECT document FROM %s WHERE run_id=$1 ORDER BY stage_id LIMIT %d`,
		store.stages, orchestration.MaximumStageCount)
	rows, err := store.executor(ctx).QueryContext(ctx, query, runID)
	if err != nil {
		return nil, provider(ctx, err, operation)
	}
	return scanStages(ctx, rows, operation)
}

// GetStages fetches a named subset.
//
// Missing ids are omitted rather than reported: the caller derives the list from
// the compiled graph, so a gap means a stage this run has not materialized yet,
// which is a normal state during StartRun and not a fault.
func (store *Store) GetStages(ctx context.Context, runID string, stageIDs []string) ([]orchestration.StageRecord, error) {
	const operation = "orchestration.postgres.GetStages"
	if err := store.validate(ctx, operation); err != nil {
		return nil, err
	}
	if len(stageIDs) == 0 {
		return nil, nil
	}
	if len(stageIDs) > MaximumStageFetch {
		return nil, domainError(ctx, faults.CodeResourceExhausted,
			"stage_lookup_bound", "stage lookup exceeds the maximum stage count", operation)
	}
	// Placeholders rather than a joined literal: the ids reach the database as
	// bound parameters, so a stage id can never be read as SQL.
	placeholders := make([]string, 0, len(stageIDs))
	arguments := make([]any, 0, len(stageIDs)+1)
	arguments = append(arguments, runID)
	for index, stageID := range stageIDs {
		placeholders = append(placeholders, fmt.Sprintf("$%d", index+2))
		arguments = append(arguments, stageID)
	}
	query := fmt.Sprintf(`SELECT document FROM %s WHERE run_id=$1 AND stage_id IN (%s) ORDER BY stage_id`,
		store.stages, strings.Join(placeholders, ","))
	rows, err := store.executor(ctx).QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, provider(ctx, err, operation)
	}
	return scanStages(ctx, rows, operation)
}

// TransitionStage applies a state change under an optimistic precondition.
//
// The row is locked and the precondition compared inside the transaction, so two
// reconcilers racing the same completion cannot both win. A transition to the
// state already held replays rather than conflicting, which is what makes an
// at-least-once redelivery safe to hand straight here.
func (store *Store) TransitionStage(
	ctx context.Context,
	runID, stageID string,
	to orchestration.StageState,
	expected resourceversion.Version,
	now time.Time,
) (orchestration.StageRecord, bool, error) {
	const operation = "orchestration.postgres.TransitionStage"
	if err := store.validate(ctx, operation); err != nil {
		return orchestration.StageRecord{}, false, err
	}
	if !to.Valid() {
		return orchestration.StageRecord{}, false, domainError(ctx, faults.CodeInvalidArgument,
			"stage_state_invalid", "stage state is unrecognized", operation)
	}
	if now.IsZero() {
		return orchestration.StageRecord{}, false, domainError(ctx, faults.CodeInvalidArgument,
			"stage_time_invalid", "stage transition time is required", operation)
	}
	updatedAt := now.Round(0).UTC()
	result, err := runMutation(ctx, store, operation, func(txContext context.Context) (stageResult, error) {
		current, found, lookupErr := store.lockStage(txContext, runID, stageID, operation)
		if lookupErr != nil {
			return stageResult{}, lookupErr
		}
		if !found {
			return stageResult{}, domainError(txContext, faults.CodeNotFound,
				"stage_not_found", "stage was not found", operation)
		}
		if current.State == to {
			return stageResult{record: current, replayed: true}, nil
		}
		if !expected.IsZero() && current.Version.String() != expected.String() {
			return stageResult{}, domainError(txContext, faults.CodeConflict,
				"stage_version_stale", "stage resource version is stale", operation)
		}
		if !orchestration.CanTransitionStage(current.State, to) {
			return stageResult{}, domainError(txContext, faults.CodeConflict,
				"stage_transition_invalid", "stage cannot make this transition", operation)
		}
		updated := current
		updated.State = to
		updated.UpdatedAt = updatedAt
		if to == orchestration.StagePreparing {
			// Counted when work is about to start, so a stage that crashed
			// between counting and launching still charged the attempt.
			// Under-counting is what would let a crash-looping stage retry
			// past its budget forever.
			updated.Attempts = current.Attempts + 1
		}
		// Sealed through the same rule the memory adapter uses, so a record
		// written by one adapter and read by the other passes its own seal
		// check. Open-coding the version here is how the two would drift.
		sealed, sealErr := orchestration.SealStage(updated, current.Version.Generation()+1)
		if sealErr != nil {
			return stageResult{}, sealErr
		}
		updated = sealed
		document, marshalErr := marshalDocument(txContext, updated, operation)
		if marshalErr != nil {
			return stageResult{}, marshalErr
		}
		attempts, attemptsErr := sqlUint(txContext, uint64(updated.Attempts), "attempts", operation)
		if attemptsErr != nil {
			return stageResult{}, attemptsErr
		}
		nextVersion, nextGeneration := stageVersionColumns(updated.Version)
		query := fmt.Sprintf(`UPDATE %s SET
state=$3,attempts=$4,updated_at=$5,resource_version=$6,resource_generation=$7,document=$8::jsonb,written_at=$5
WHERE run_id=$1 AND stage_id=$2`, store.stages)
		if _, execErr := store.executor(txContext).ExecContext(txContext, query,
			runID, stageID, string(updated.State), attempts, updatedAt,
			nextVersion, nextGeneration, document,
		); execErr != nil {
			return stageResult{}, provider(txContext, execErr, operation)
		}
		if emitErr := store.emitStage(txContext, "orchestration.stage."+string(to), updated); emitErr != nil {
			return stageResult{}, emitErr
		}
		return stageResult{record: updated}, nil
	})
	if err != nil {
		return orchestration.StageRecord{}, false, err
	}
	return result.record, result.replayed, nil
}

func (store *Store) lockStage(ctx context.Context, runID, stageID, operation string) (orchestration.StageRecord, bool, error) {
	var document []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE run_id=$1 AND stage_id=$2 FOR UPDATE`, store.stages),
		runID, stageID).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return orchestration.StageRecord{}, false, nil
	}
	if err != nil {
		return orchestration.StageRecord{}, false, provider(ctx, err, operation+".Lock")
	}
	record, decodeErr := decodeDocument[stageDocument](ctx, document, operation)
	if decodeErr != nil {
		return orchestration.StageRecord{}, false, decodeErr
	}
	return orchestration.StageRecord(record), true, nil
}

func scanStages(ctx context.Context, rows *sql.Rows, operation string) ([]orchestration.StageRecord, error) {
	defer func() { _ = rows.Close() }()
	records := make([]orchestration.StageRecord, 0, 16)
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, provider(ctx, err, operation)
		}
		decoded, err := decodeDocument[stageDocument](ctx, document, operation)
		if err != nil {
			return nil, err
		}
		records = append(records, orchestration.StageRecord(decoded))
	}
	if err := rows.Err(); err != nil {
		return nil, provider(ctx, err, operation)
	}
	return records, nil
}

// stageVersionColumns projects the sealed version into its two columns. The
// generation is stored separately so the table's CHECK can prove the two agree;
// a zero version is stored as NULL rather than as an empty string so the
// constraint distinguishes "unsealed" from "sealed to nothing".
func stageVersionColumns(version resourceversion.Version) (any, any) {
	if version.IsZero() {
		return nil, nil
	}
	return version.String(), int64(version.Generation())
}

// stageDocument adapts StageRecord to decodeDocument's constraint.
type stageDocument orchestration.StageRecord

func (document stageDocument) Validate() error {
	return orchestration.StageRecord(document).Validate()
}
