// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"testing"

	"go.mindclade.dev/libs/go/faults"
)

// The Rust worker owns this table. If Go and Rust disagree about a single edge,
// the control plane either rejects a status the worker legitimately sent or
// accepts one it never should have, so every edge is asserted individually
// against libs/rust/worker_runtime/src/machine.rs.
func TestAttemptTransitionsMirrorTheWorkerProtocol(t *testing.T) {
	allowed := map[AttemptState][]AttemptState{
		AttemptCreated:    {AttemptStarting, AttemptCancelling},
		AttemptStarting:   {AttemptReady, AttemptRecovering, AttemptCancelling, AttemptFailed},
		AttemptReady:      {AttemptLeased, AttemptRecovering, AttemptCancelling, AttemptFailed},
		AttemptLeased:     {AttemptRunning, AttemptCancelling, AttemptFailed},
		AttemptRunning:    {AttemptDraining, AttemptCommitting, AttemptRecovering, AttemptCancelling, AttemptFailed},
		AttemptDraining:   {AttemptCommitting, AttemptCancelling, AttemptFailed},
		AttemptCommitting: {AttemptCompleted, AttemptCancelling, AttemptFailed},
		AttemptRecovering: {AttemptReady, AttemptCancelling, AttemptFailed},
		AttemptCancelling: {AttemptCancelled},
	}
	every := []AttemptState{
		AttemptCreated, AttemptStarting, AttemptReady, AttemptLeased, AttemptRunning,
		AttemptDraining, AttemptCommitting, AttemptCompleted, AttemptRecovering,
		AttemptCancelling, AttemptCancelled, AttemptFailed,
	}
	for _, from := range every {
		permitted := map[AttemptState]bool{}
		for _, to := range allowed[from] {
			permitted[to] = true
		}
		for _, to := range every {
			if got := CanTransition(from, to); got != permitted[to] {
				t.Fatalf("CanTransition(%s, %s) = %v, want %v", from, to, got, permitted[to])
			}
		}
	}
}

// Ready is where a recovered attempt re-enters execution. Allowing any other
// state to reach it would let a failed attempt be re-armed in place, keeping its
// old fence and ticket instead of being reissued.
func TestAttemptReadyIsReachableOnlyFromStartingAndRecovering(t *testing.T) {
	for _, from := range []AttemptState{
		AttemptCreated, AttemptReady, AttemptLeased, AttemptRunning, AttemptDraining,
		AttemptCommitting, AttemptCompleted, AttemptCancelling, AttemptCancelled, AttemptFailed,
	} {
		if CanTransition(from, AttemptReady) {
			t.Fatalf("%s must not reach ready", from)
		}
	}
	if !CanTransition(AttemptStarting, AttemptReady) || !CanTransition(AttemptRecovering, AttemptReady) {
		t.Fatal("starting and recovering must reach ready")
	}
}

// Drain is graceful. A draining attempt still has outputs to publish, so it must
// keep its path to Committing; treating drain as cancellation would discard work
// that had already succeeded.
func TestDrainingStillReachesCommitting(t *testing.T) {
	if !CanTransition(AttemptDraining, AttemptCommitting) {
		t.Fatal("a draining attempt must still be able to commit")
	}
}

// Every cancellation gets a durable intent before it gets an outcome.
func TestCancelledIsReachableOnlyThroughCancelling(t *testing.T) {
	for _, from := range []AttemptState{
		AttemptCreated, AttemptStarting, AttemptReady, AttemptLeased, AttemptRunning,
		AttemptDraining, AttemptCommitting, AttemptCompleted, AttemptRecovering, AttemptFailed,
	} {
		if CanTransition(from, AttemptCancelled) {
			t.Fatalf("%s must not reach cancelled directly", from)
		}
	}
	if !CanTransition(AttemptCancelling, AttemptCancelled) {
		t.Fatal("cancelling must reach cancelled")
	}
}

func TestTerminalAttemptStatesHaveNoOutgoingEdges(t *testing.T) {
	for _, terminal := range []AttemptState{AttemptCompleted, AttemptCancelled, AttemptFailed} {
		if !terminal.Terminal() {
			t.Fatalf("%s must report terminal", terminal)
		}
		for to := range attemptTransitions {
			if CanTransition(terminal, to) {
				t.Fatalf("terminal %s must not transition to %s", terminal, to)
			}
		}
	}
}

// A lost lease returns the stage to the queue for another attempt, so this edge
// is what makes a preempted node recoverable rather than fatal.
func TestStageReturnsToQueueAfterALostAttempt(t *testing.T) {
	if !CanTransitionStage(StageRunning, StageQueued) {
		t.Fatal("a running stage must be able to return to the queue")
	}
	if CanTransitionStage(StageBlocked, StageRunning) {
		t.Fatal("a blocked stage must not start without being queued")
	}
	if CanTransitionStage(StageQueued, StageSucceeded) {
		t.Fatal("a stage must not succeed without running")
	}
}

func TestStageTerminalStatesAreClosed(t *testing.T) {
	for _, terminal := range []StageState{StageSucceeded, StageFailed, StageCancelled} {
		if !terminal.Terminal() {
			t.Fatalf("%s must report terminal", terminal)
		}
		for to := range stageTransitions {
			if CanTransitionStage(terminal, to) {
				t.Fatalf("terminal %s must not transition to %s", terminal, to)
			}
		}
	}
}

// Resource exhaustion must never consume an attempt: the work was refused, not
// tried. Getting this wrong burns a stage's whole budget on a full queue.
func TestClassifyDistinguishesBackpressureFromFailure(t *testing.T) {
	cases := map[string]struct {
		err  error
		want Disposition
	}{
		"exhausted reschedules": {
			err:  faults.New(faults.CodeResourceExhausted, "queue full", faults.WithRetryPolicy(faults.NoRetry())),
			want: DispositionReschedule,
		},
		"aborted reschedules": {
			err:  faults.New(faults.CodeAborted, "serialization failure", faults.WithRetryPolicy(faults.NoRetry())),
			want: DispositionReschedule,
		},
		"retryable unavailable retries": {
			err:  faults.New(faults.CodeUnavailable, "provider down", faults.WithRetryPolicy(faults.BackoffRetry(3))),
			want: DispositionRetry,
		},
		"unavailable without a retry policy is terminal": {
			err:  faults.New(faults.CodeUnavailable, "provider down", faults.WithRetryPolicy(faults.NoRetry())),
			want: DispositionTerminal,
		},
		"stale fence is terminal": {
			err:  faults.New(faults.CodeConflict, "stale fencing token", faults.WithRetryPolicy(faults.NoRetry())),
			want: DispositionTerminal,
		},
		"denied is terminal": {
			err:  faults.New(faults.CodePermissionDenied, "ticket mismatch", faults.WithRetryPolicy(faults.NoRetry())),
			want: DispositionTerminal,
		},
		"invalid input is terminal": {
			err:  faults.New(faults.CodeInvalidArgument, "bad spec", faults.WithRetryPolicy(faults.NoRetry())),
			want: DispositionTerminal,
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Classify(testCase.err); got != testCase.want {
				t.Fatalf("Classify() = %s, want %s", got, testCase.want)
			}
		})
	}
	if Classify(nil) != DispositionTerminal {
		t.Fatal("a nil cause must not be treated as retryable")
	}
}

// The label set must stay bounded. A metric keyed on the fault reason would grow
// a new series for every failure mode anyone adds, which is the cardinality
// failure the sibling domains' SLO contracts exist to prevent.
func TestClassifyObservedReportsABoundedLabelSet(t *testing.T) {
	var seen []ClassificationEvent
	observer := ObserverFunc(func(event ClassificationEvent) {
		seen = append(seen, event)
	})
	err := faults.New(faults.CodeResourceExhausted, "queue full",
		faults.WithReason("a_reason_nobody_should_key_a_metric_on"),
		faults.WithRetryPolicy(faults.NoRetry()))

	if got := ClassifyObserved(err, observer); got != DispositionReschedule {
		t.Fatalf("disposition = %s, want reschedule", got)
	}
	if len(seen) != 1 {
		t.Fatalf("observed %d events, want 1", len(seen))
	}
	if seen[0].Disposition != DispositionReschedule {
		t.Fatalf("event disposition = %s, want reschedule", seen[0].Disposition)
	}
	if seen[0].Code != faults.CodeResourceExhausted {
		t.Fatalf("event code = %s, want resource_exhausted", seen[0].Code)
	}
}

// Telemetry is never a precondition for running work, so an absent observer and
// a typed-nil one must both classify normally rather than panic.
func TestClassifyObservedToleratesAnAbsentObserver(t *testing.T) {
	err := faults.New(faults.CodeConflict, "stale fence",
		faults.WithReason("stale_fencing_token"), faults.WithRetryPolicy(faults.NoRetry()))
	if got := ClassifyObserved(err, nil); got != DispositionTerminal {
		t.Fatalf("disposition = %s, want terminal", got)
	}
	var typedNil ObserverFunc
	if got := ClassifyObserved(err, typedNil); got != DispositionTerminal {
		t.Fatalf("disposition with a typed-nil observer = %s, want terminal", got)
	}
}
