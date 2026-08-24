// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"context"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

// Service is the transport-neutral domain surface behind
// mindclade.orchestration.v1.RunService and the reconciliation decisions the
// controller role drives.
//
// It is a plain struct with exported dependencies rather than a constructor,
// matching the other control/ domains: the composition root fills it in, and
// every method re-checks its dependencies so a partially wired binary fails
// closed on first use instead of panicking.
type Service struct {
	Repository Repository
	Clock      clock.Clock
}

func (service Service) validate(ctx context.Context) error {
	if ctx == nil {
		return invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if service.Repository == nil || nilInterface(service.Repository) {
		return unavailable("repository_unavailable", "orchestration repository is unavailable", nil)
	}
	if service.Clock != nil && nilInterface(service.Clock) {
		return unavailable("clock_unavailable", "orchestration clock is unavailable", nil)
	}
	return nil
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return clock.RealClock{}.Now().Round(0).UTC()
	}
	return service.Clock.Now().Round(0).UTC()
}

// PublishWorkflow compiles a submitted plan and stores it immutably.
//
// The workflow identity is minted here rather than accepted, because a
// caller-supplied id could collide with a published plan and silently redefine
// a graph that is already running.
func (service Service) PublishWorkflow(ctx context.Context, request CompileRequest) (CompiledWorkflow, bool, error) {
	if err := service.validate(ctx); err != nil {
		return CompiledWorkflow{}, false, err
	}
	now := service.now()
	id, err := identifiers.NewIDAt(identifiers.MustParseKind("workflow"), now)
	if err != nil {
		return CompiledWorkflow{}, false, unavailable("workflow_id_unavailable", "workflow identifier is unavailable", err)
	}
	compiled, err := Compile(request, id)
	if err != nil {
		return CompiledWorkflow{}, false, err
	}
	if _, replayed, err := service.Repository.PutWorkflow(ctx, compiled, now); err != nil {
		return CompiledWorkflow{}, false, err
	} else if replayed {
		return compiled, true, nil
	}
	return compiled, false, nil
}

// StartRun materializes the stage records for a published workflow.
//
// Every stage starts blocked, including the roots. Ready is what promotes them,
// so there is one rule for becoming runnable rather than a separate special case
// for stages that happen to have no parents.
func (service Service) StartRun(ctx context.Context, workflowID, runID, jobID string) ([]StageRecord, error) {
	if err := service.validate(ctx); err != nil {
		return nil, err
	}
	if err := validateID(runID, "run", "run_id"); err != nil {
		return nil, err
	}
	if err := validateID(jobID, "job", "job_id"); err != nil {
		return nil, err
	}
	compiled, err := service.Repository.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	now := service.now()
	records := make([]StageRecord, 0, compiled.Graph.Len())
	for _, stageID := range compiled.Graph.Order() {
		// NewStage rather than a struct literal: a literal carries no sealed
		// version, and PutStage validates the seal, so every StartRun refused
		// its own stages with stage_version_invalid. Nothing reached AdmitReady
		// to notice, because until the placement seam existed nothing drove
		// this path end to end.
		record, err := NewStage(runID, jobID, stageID, now)
		if err != nil {
			return nil, err
		}
		stored, _, err := service.Repository.PutStage(ctx, record)
		if err != nil {
			return nil, err
		}
		records = append(records, stored)
	}
	return records, nil
}

// AdmitReady promotes every stage whose dependencies have all succeeded.
//
// It returns the stages it moved. The caller does NOT enqueue them: a promotion
// appends its placement item inside the repository's transaction (see Enqueuer),
// because an enqueue this service issued after TransitionStage returned would be
// lost by a crash in between. Reporting the moved set rather than the ready set
// still matters under concurrency -- a stage another reconciler already promoted
// comes back as replayed and is excluded, so two reconcilers cannot place the
// same stage twice -- but the set is now a report of what happened rather than a
// work list.
func (service Service) AdmitReady(ctx context.Context, workflowID, runID string) ([]StageRecord, error) {
	if err := service.validate(ctx); err != nil {
		return nil, err
	}
	compiled, err := service.Repository.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	records, err := service.Repository.ListStages(ctx, runID)
	if err != nil {
		return nil, err
	}
	states := make(map[string]StageState, len(records))
	versions := make(map[string]resourceversion.Version, len(records))
	for _, record := range records {
		states[record.StageID] = record.State
		versions[record.StageID] = record.Version
	}
	now := service.now()
	promoted := make([]StageRecord, 0, len(records))
	for _, stageID := range compiled.Graph.Ready(states) {
		updated, replayed, err := service.Repository.TransitionStage(
			ctx, runID, stageID, StageQueued, versions[stageID], now)
		if err != nil {
			return nil, err
		}
		if !replayed {
			promoted = append(promoted, updated)
		}
	}
	return promoted, nil
}

// CompleteStage records a terminal stage outcome and releases whatever it
// unblocks.
//
// Only success releases dependents. A failed stage leaves its children blocked
// because the outputs they consume do not exist; clearing them is what
// cancelling the run is for.
//
// The stages it returns were promoted to queued, so each one has already placed
// its own work item inside the repository's transaction. As with AdmitReady, the
// caller reports on them rather than enqueueing them.
func (service Service) CompleteStage(ctx context.Context, workflowID, runID, stageID string, outcome StageState) ([]StageRecord, error) {
	if err := service.validate(ctx); err != nil {
		return nil, err
	}
	if !outcome.Terminal() {
		return nil, invalid("stage_outcome_invalid", "completing a stage requires a terminal state", nil)
	}
	compiled, err := service.Repository.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	current, err := service.Repository.GetStage(ctx, runID, stageID)
	if err != nil {
		return nil, err
	}
	now := service.now()
	if _, _, err := service.Repository.TransitionStage(ctx, runID, stageID, outcome, current.Version, now); err != nil {
		return nil, err
	}
	if outcome != StageSucceeded {
		return nil, nil
	}
	// Only the completed stage's children and their parents can change state
	// here, so fetch exactly that set. Scanning the whole run would make a
	// single completion cost O(stages) against a graph bounded at 4096, on a
	// path that runs once per stage per run.
	scope := compiled.Graph.CompletionScope(stageID)
	records, err := service.Repository.GetStages(ctx, runID, scope)
	if err != nil {
		return nil, err
	}
	states := make(map[string]StageState, len(records))
	versions := make(map[string]resourceversion.Version, len(records))
	for _, record := range records {
		states[record.StageID] = record.State
		versions[record.StageID] = record.Version
	}
	states[stageID] = outcome
	promoted := make([]StageRecord, 0, len(records))
	for _, next := range compiled.Graph.Unblocked(stageID, states) {
		updated, replayed, err := service.Repository.TransitionStage(
			ctx, runID, next, StageQueued, versions[next], now)
		if err != nil {
			return nil, err
		}
		if !replayed {
			promoted = append(promoted, updated)
		}
	}
	return promoted, nil
}

// Cancel records the intent and returns the stages it reaches.
//
// The intent is durable before anything stops, so a control plane that crashes
// mid-cancellation resumes cancelling rather than forgetting that it was asked.
//
// The stage fetch is scoped to what the cancellation can actually reach, which
// is what the origin already decides. A run-scoped stop (client, operator,
// deadline) walks the graph and needs every stage. An attempt-scoped one --
// claim loss, preemption -- touches exactly the one stage it names and is the
// origin that arrives per lost lease, so listing a run bounded at 4096 stages to
// cancel one of them made a node preemption cost O(stages) per attempt it took
// down. Propagate's attempt-scoped arm reads only states[intent.StageID], so the
// narrower map is the whole map it consults; see the note on Propagate before
// widening what either arm reads.
func (service Service) Cancel(ctx context.Context, workflowID string, intent CancellationIntent) ([]StageRecord, bool, error) {
	if err := service.validate(ctx); err != nil {
		return nil, false, err
	}
	compiled, err := service.Repository.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, false, err
	}
	stored, replayed, err := service.Repository.RecordCancellation(ctx, intent)
	if err != nil {
		return nil, false, err
	}
	records, err := service.cancellationScope(ctx, stored)
	if err != nil {
		return nil, false, err
	}
	states := make(map[string]StageState, len(records))
	versions := make(map[string]resourceversion.Version, len(records))
	for _, record := range records {
		states[record.StageID] = record.State
		versions[record.StageID] = record.Version
	}
	affected, err := Propagate(compiled.Graph, stored, states)
	if err != nil {
		return nil, false, err
	}
	now := service.now()
	cancelling := make([]StageRecord, 0, len(affected))
	for _, stageID := range affected {
		next, changed, err := CancelStage(states[stageID], now)
		if err != nil {
			return nil, false, err
		}
		if !changed {
			continue
		}
		updated, alreadyApplied, err := service.Repository.TransitionStage(
			ctx, stored.RunID, stageID, next, versions[stageID], now)
		if err != nil {
			return nil, false, err
		}
		if !alreadyApplied {
			cancelling = append(cancelling, updated)
		}
	}
	return cancelling, replayed, nil
}

// cancellationScope fetches exactly the stages this cancellation can reach.
//
// The scope is read off the recorded intent rather than the request, because
// RecordCancellation is the point the origin becomes durable: a replayed
// delivery re-derives its scope from the stored record and therefore fetches
// the same set the first delivery did.
//
// A stage the fetch does not return is absent from the state map, and an absent
// stage is not a terminal one -- Propagate names it, and CancelStage then
// refuses the empty state as stage_state_invalid. That is the same answer
// ListStages gives for a run whose stage row is missing, which is why narrowing
// the fetch changes cost and not behaviour.
func (service Service) cancellationScope(ctx context.Context, stored CancellationIntent) ([]StageRecord, error) {
	if stored.Origin.scopesRun() {
		return service.Repository.ListStages(ctx, stored.RunID)
	}
	return service.Repository.GetStages(ctx, stored.RunID, []string{stored.StageID})
}

// ReconcileAttempt applies one launcher observation to durable attempt state.
//
// The fence and the sequence are both checked before anything is written: a
// worker that lost its lease keeps running until it notices, and a status that
// arrived twice or out of order must not move state backwards.
func (service Service) ReconcileAttempt(ctx context.Context, item WorkItem, observation Observation, fence uint64) (AttemptRecord, error) {
	if err := service.validate(ctx); err != nil {
		return AttemptRecord{}, err
	}
	if err := item.Validate(); err != nil {
		return AttemptRecord{}, err
	}
	if err := observation.Validate(); err != nil {
		return AttemptRecord{}, err
	}
	record, err := service.Repository.GetAttempt(ctx, item.RunID, item.StageID, item.Attempt)
	if err != nil {
		return AttemptRecord{}, err
	}
	updated, err := record.ApplyStatus(fence, observation.Sequence, observation.State, service.now())
	if err != nil {
		return AttemptRecord{}, err
	}
	if observation.State == AttemptFailed && observation.Failure != nil {
		updated, err = updated.Fail(observation.Failure, service.now())
		if err != nil {
			return AttemptRecord{}, err
		}
	}
	stored, _, err := service.Repository.PutAttempt(ctx, updated)
	if err != nil {
		return AttemptRecord{}, err
	}
	return stored, nil
}
