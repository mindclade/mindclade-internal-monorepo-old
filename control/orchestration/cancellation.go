// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"time"

	"go.mindclade.dev/libs/go/idempotency"
)

// CancellationOrigin records who asked. It is not decoration: an operator
// cancelling a run and a claim loss cancelling one attempt have to be
// distinguishable, because the first should stop the whole graph and the second
// should let the stage try again.
type CancellationOrigin string

const (
	// OriginClient is an explicit CancelRun request.
	OriginClient CancellationOrigin = "client"
	// OriginOperator is an administrative stop.
	OriginOperator CancellationOrigin = "operator"
	// OriginClaimLoss is the work queue reporting that a replacement has taken
	// the claim. It cancels the attempt only.
	OriginClaimLoss CancellationOrigin = "claim_loss"
	// OriginDeadline is the stage or ticket deadline elapsing.
	OriginDeadline CancellationOrigin = "deadline"
	// OriginPreemption is the scheduler reclaiming capacity.
	OriginPreemption CancellationOrigin = "preemption"
)

func (origin CancellationOrigin) Valid() bool {
	switch origin {
	case OriginClient, OriginOperator, OriginClaimLoss, OriginDeadline, OriginPreemption:
		return true
	}
	return false
}

// scopesRun reports whether this origin stops the whole graph. Claim loss and
// preemption take an attempt away without saying the work is unwanted, so they
// are attempt-scoped and the stage remains eligible for another attempt.
func (origin CancellationOrigin) scopesRun() bool {
	return origin == OriginClient || origin == OriginOperator || origin == OriginDeadline
}

// CancellationIntent is the durable record that cancellation was requested. It
// is written before anything is stopped, so a control plane that crashes
// mid-cancellation resumes cancelling rather than forgetting.
type CancellationIntent struct {
	RunID       string
	StageID     string
	Origin      CancellationOrigin
	Reason      string
	Idempotency idempotency.Identity
	RequestedAt time.Time
}

func (intent CancellationIntent) Validate() error {
	if err := validateID(intent.RunID, "run", "run_id"); err != nil {
		return err
	}
	if intent.StageID != "" {
		if err := validateID(intent.StageID, "stage", "stage_id"); err != nil {
			return err
		}
	}
	if !intent.Origin.Valid() {
		return invalid("cancellation_origin_invalid", "cancellation origin is unrecognized", nil)
	}
	if err := validateReason(intent.Reason); err != nil {
		return err
	}
	if err := intent.Idempotency.Validate(); err != nil {
		return invalid("cancellation_idempotency_invalid", "cancellation idempotency identity is invalid", err)
	}
	if intent.RequestedAt.IsZero() {
		return invalid("cancellation_time_invalid", "cancellation request time is required", nil)
	}
	// A run-scoped origin cancels the graph, so naming one stage would describe
	// two different operations with one record.
	if intent.Origin.scopesRun() && intent.StageID != "" {
		return invalid("cancellation_scope_mismatch", "a run-scoped cancellation cannot name a single stage", nil)
	}
	if !intent.Origin.scopesRun() && intent.StageID == "" {
		return invalid("cancellation_stage_required", "an attempt-scoped cancellation must name its stage", nil)
	}
	return nil
}

// CancelStage moves a stage toward cancellation.
//
// A stage that already reached a terminal state is left alone and reported as
// unchanged. Cancelling a succeeded stage would discard a result that has
// already been published and that downstream stages may already have consumed.
func CancelStage(current StageState, now time.Time) (StageState, bool, error) {
	if !current.Valid() {
		return "", false, invalid("stage_state_invalid", "stage state is unrecognized", nil)
	}
	if now.IsZero() {
		return "", false, invalid("cancellation_time_invalid", "cancellation time is required", nil)
	}
	if current.Terminal() || current == StageCancelling {
		return current, false, nil
	}
	if !CanTransitionStage(current, StageCancelling) {
		return "", false, conflict("stage_cancellation_invalid", "stage cannot enter cancellation from its current state")
	}
	return StageCancelling, true, nil
}

// CancelAttempt cancels one attempt, honoring the fence.
//
// Committing is explicitly cancellable, because the worker protocol allows it:
// the attempt has not published yet, and stopping it before it does is the whole
// value of a cancellation that arrives late. Completed is not — its outputs are
// durable, and "cancelling" them would be a deletion wearing another name.
func CancelAttempt(record AttemptRecord, fence uint64, now time.Time) (AttemptRecord, bool, error) {
	if err := record.Validate(); err != nil {
		return AttemptRecord{}, false, err
	}
	if fence != 0 && fence != record.FencingToken {
		return AttemptRecord{}, false, conflict("stale_fencing_token", "cancellation carries a stale fencing token")
	}
	if record.State.Terminal() {
		return record, false, nil
	}
	if record.State == AttemptCancelling {
		return record, false, nil
	}
	updated, err := record.Transition(AttemptCancelling, now)
	if err != nil {
		return AttemptRecord{}, false, err
	}
	return updated, true, nil
}

// Propagate returns the stages a cancellation reaches.
//
// Only non-terminal stages are named. A run-scoped cancellation reaches all of
// them; an attempt-scoped one reaches exactly the stage it names, which is what
// keeps a preempted node from tearing down the rest of the graph.
//
// The two arms read `states` differently, and the difference is load-bearing.
// The attempt-scoped arm indexes exactly one key, states[intent.StageID], so a
// caller may hand it a map holding only that stage -- Service.Cancel does, so a
// lost lease does not cost a full run listing. The run-scoped arm ranges the
// whole graph and treats a missing key as the zero StageState, which is not
// terminal and is therefore named. Do not teach the attempt-scoped arm to read
// another stage without widening the fetch behind it: against a truncated map
// every unlisted stage reads as non-terminal, and cancelling one attempt would
// come back claiming the whole run.
func Propagate(graph Graph, intent CancellationIntent, states map[string]StageState) ([]string, error) {
	if err := intent.Validate(); err != nil {
		return nil, err
	}
	if !intent.Origin.scopesRun() {
		if _, ok := graph.Stage(intent.StageID); !ok {
			return nil, notFound("stage_not_found", "cancellation names a stage outside the workflow")
		}
		if states[intent.StageID].Terminal() {
			return nil, nil
		}
		return []string{intent.StageID}, nil
	}
	affected := make([]string, 0, graph.Len())
	for _, id := range graph.Order() {
		if !states[id].Terminal() {
			affected = append(affected, id)
		}
	}
	return affected, nil
}
