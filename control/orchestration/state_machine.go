// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import "go.mindclade.dev/libs/go/faults"

// AttemptState mirrors mindclade.runtime.v1.WorkerState. The Rust worker owns
// the authoritative transition table in libs/rust/worker_runtime/src/machine.rs
// and reports these states on the wire, so Go reproduces that vocabulary rather
// than inventing a second one: a control plane whose idea of "running" differed
// from the worker's would disagree with every status it received.
type AttemptState string

const (
	AttemptCreated    AttemptState = "created"
	AttemptStarting   AttemptState = "starting"
	AttemptReady      AttemptState = "ready"
	AttemptLeased     AttemptState = "leased"
	AttemptRunning    AttemptState = "running"
	AttemptDraining   AttemptState = "draining"
	AttemptCommitting AttemptState = "committing"
	AttemptCompleted  AttemptState = "completed"
	AttemptRecovering AttemptState = "recovering"
	AttemptCancelling AttemptState = "cancelling"
	AttemptCancelled  AttemptState = "cancelled"
	AttemptFailed     AttemptState = "failed"
)

func (state AttemptState) Valid() bool {
	switch state {
	case AttemptCreated, AttemptStarting, AttemptReady, AttemptLeased,
		AttemptRunning, AttemptDraining, AttemptCommitting, AttemptCompleted,
		AttemptRecovering, AttemptCancelling, AttemptCancelled, AttemptFailed:
		return true
	}
	return false
}

// Terminal mirrors worker_runtime::state::is_terminal.
func (state AttemptState) Terminal() bool {
	return state == AttemptCompleted || state == AttemptCancelled || state == AttemptFailed
}

// attemptTransitions is machine.rs `allowed`, transcribed. Four properties of
// this table are load-bearing and easy to lose by "tidying" it:
//
//   - Ready is reachable only from Starting and Recovering. A failed attempt is
//     never re-armed in place; recovery mints a new ticket and a newer fence.
//   - Draining does NOT trip cancellation. Drain is graceful, so a draining
//     attempt may still reach Committing and publish its outputs.
//   - Cancelled is reachable only through Cancelling, so every cancellation has
//     a durable intent record before it has an outcome.
//   - No terminal state has an outgoing edge.
var attemptTransitions = map[AttemptState]map[AttemptState]bool{
	AttemptCreated:    {AttemptStarting: true, AttemptCancelling: true},
	AttemptStarting:   {AttemptReady: true, AttemptRecovering: true, AttemptCancelling: true, AttemptFailed: true},
	AttemptReady:      {AttemptLeased: true, AttemptRecovering: true, AttemptCancelling: true, AttemptFailed: true},
	AttemptLeased:     {AttemptRunning: true, AttemptCancelling: true, AttemptFailed: true},
	AttemptRunning:    {AttemptDraining: true, AttemptCommitting: true, AttemptRecovering: true, AttemptCancelling: true, AttemptFailed: true},
	AttemptDraining:   {AttemptCommitting: true, AttemptCancelling: true, AttemptFailed: true},
	AttemptCommitting: {AttemptCompleted: true, AttemptCancelling: true, AttemptFailed: true},
	AttemptRecovering: {AttemptReady: true, AttemptCancelling: true, AttemptFailed: true},
	AttemptCancelling: {AttemptCancelled: true},
}

// CanTransition reports whether the worker protocol permits this edge.
func CanTransition(from, to AttemptState) bool {
	return attemptTransitions[from][to]
}

// StageState is the durable stage vocabulary. It is derived from
// mindclade.orchestration.v1.RunState rather than from the worker vocabulary
// above: a stage is a unit of the run graph and outlives the attempts that
// execute it, so it must be able to say "waiting on a dependency" and "retrying"
// — neither of which a single worker can express.
type StageState string

const (
	// StageBlocked is the only state with no RunState counterpart. A run has
	// nothing upstream of it; a stage does, and conflating "blocked on a
	// dependency" with "queued for capacity" would make the scheduler admit
	// work whose inputs do not exist yet.
	StageBlocked    StageState = "blocked"
	StageQueued     StageState = "queued"
	StagePreparing  StageState = "preparing"
	StageRunning    StageState = "running"
	StageSucceeded  StageState = "succeeded"
	StageFailed     StageState = "failed"
	StageCancelling StageState = "cancelling"
	StageCancelled  StageState = "cancelled"
)

func (state StageState) Valid() bool {
	switch state {
	case StageBlocked, StageQueued, StagePreparing, StageRunning,
		StageSucceeded, StageFailed, StageCancelling, StageCancelled:
		return true
	}
	return false
}

func (state StageState) Terminal() bool {
	return state == StageSucceeded || state == StageFailed || state == StageCancelled
}

// stageTransitions permits Running -> Queued because a stage outlives its
// attempts: a lease lost to a preempted node returns the stage to the queue for
// a new attempt. Succeeded is reachable only from Running, so a stage cannot be
// declared complete without having executed.
var stageTransitions = map[StageState]map[StageState]bool{
	StageBlocked:    {StageQueued: true, StageCancelling: true, StageFailed: true},
	StageQueued:     {StagePreparing: true, StageCancelling: true, StageFailed: true},
	StagePreparing:  {StageRunning: true, StageQueued: true, StageCancelling: true, StageFailed: true},
	StageRunning:    {StageSucceeded: true, StageFailed: true, StageQueued: true, StageCancelling: true},
	StageCancelling: {StageCancelled: true},
}

// CanTransitionStage reports whether a stage may move between these states.
func CanTransitionStage(from, to StageState) bool {
	return stageTransitions[from][to]
}

// Disposition is the decision a reconciler reaches about a failed attempt.
type Disposition string

const (
	// DispositionRetry schedules another attempt within the stage's budget.
	DispositionRetry Disposition = "retry"
	// DispositionReschedule returns the work to the queue without charging an
	// attempt against a fault the work did not cause.
	DispositionReschedule Disposition = "reschedule"
	// DispositionTerminal fails the stage now; no further attempt can succeed.
	DispositionTerminal Disposition = "terminal"
)

// Classify maps a failure onto the retry policy the failure model in
// docs/architecture/system-design-reference.md section 44 prescribes.
//
// The classification is read off the structured fault rather than guessed from
// the error string, and the default is terminal. An error is not retryable
// merely because it looks transient: replaying a mutation whose replay safety
// nobody established is how one duplicate becomes two committed artifacts.
func Classify(err error) Disposition {
	if err == nil {
		return DispositionTerminal
	}
	switch faults.CodeOf(err) {
	case faults.CodeResourceExhausted:
		// Backpressure, not blind retry. The work was never admitted, so it
		// costs the stage nothing and must not consume an attempt.
		return DispositionReschedule
	case faults.CodeAborted:
		// A lost race, not a bad request. The canonical case is a serializable
		// transaction that must be replayed.
		return DispositionReschedule
	case faults.CodeUnavailable, faults.CodeDeadlineExceeded, faults.CodeInternal:
		if faults.IsRetryable(err) {
			return DispositionRetry
		}
		return DispositionTerminal
	case faults.CodeConflict, faults.CodePermissionDenied, faults.CodeFailedPrecondition:
		// A stale fence or resource version is terminal FOR THIS ATTEMPT: a
		// replacement already owns the work, and retrying under the old fence
		// is precisely what fencing exists to stop.
		return DispositionTerminal
	default:
		return DispositionTerminal
	}
}

// ClassificationEvent reports one retry decision.
//
// It carries the disposition and the fault code and deliberately not the fault
// reason. Disposition has three values and Code has seventeen, so the pair is a
// bounded label set; reason is domain-authored free text with no ceiling, and a
// metric keyed on it would grow a new series for every failure mode anyone ever
// adds. The SLO contract for the sibling domains names tenant, workspace, run and
// request identifiers as forbidden labels for the same reason -- this keeps that
// discipline at the point the label is chosen rather than at the exporter.
type ClassificationEvent struct {
	Disposition Disposition
	Code        faults.Code
}

// Observer receives classification decisions. The domain defines the contract and
// the composition root adapts it to telemetry, matching workqueue.Observer and
// leadership.Observer; a domain package that imported an exporter would make its
// own tests need one.
//
// Implementations must not block: this runs on the path that decides whether a
// claimed work item retries.
type Observer interface {
	ObserveClassification(ClassificationEvent)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(ClassificationEvent)

func (function ObserverFunc) ObserveClassification(event ClassificationEvent) {
	if function != nil {
		function(event)
	}
}

// ClassifyObserved classifies a failure and reports the decision.
//
// The question this answers operationally is "are we retrying things we should
// not, or giving up on things we should retry" -- which is invisible without a
// signal at the moment of the decision, because both outcomes look like an
// ordinary stage failure downstream.
func ClassifyObserved(err error, observer Observer) Disposition {
	disposition := Classify(err)
	if observer != nil && !nilInterface(observer) {
		observer.ObserveClassification(ClassificationEvent{
			Disposition: disposition,
			Code:        faults.CodeOf(err),
		})
	}
	return disposition
}
