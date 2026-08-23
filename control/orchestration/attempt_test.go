// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"testing"
	"time"

	"go.mindclade.dev/libs/go/faults"
)

func testAttempt(t *testing.T, number uint32, fence uint64) (AttemptRecord, StageSpec) {
	t.Helper()
	spec := testStage(t, testID(t, "stage"), "fetch")
	record, err := NewAttempt(
		testID(t, "run"), testID(t, "job"), spec,
		number, fence, testID(t, "ticket"), "cpu-general", testStart,
	)
	if err != nil {
		t.Fatalf("new attempt: %v", err)
	}
	return record, spec
}

// started advances a fresh attempt to Starting. The worker protocol has no
// Created -> Failed edge -- an attempt that never started can only be cancelled
// -- so anything testing failure has to launch first.
func started(t *testing.T, record AttemptRecord) AttemptRecord {
	t.Helper()
	next, err := record.Transition(AttemptStarting, testStart.Add(time.Second))
	if err != nil {
		t.Fatalf("starting: %v", err)
	}
	return next
}

func TestNewAttemptSealsItsContent(t *testing.T) {
	record, _ := testAttempt(t, 1, 7)
	if record.State != AttemptCreated {
		t.Fatalf("state = %s, want created", record.State)
	}
	if record.Version.Generation() != 1 {
		t.Fatalf("generation = %d, want 1", record.Version.Generation())
	}
	if err := record.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	// A record whose content no longer matches its sealed digest must be
	// rejected, or a tampered row would read back as authentic.
	tampered := record
	tampered.ExecutionClass = "gpu-h100"
	if err := tampered.Validate(); err == nil {
		t.Fatal("a record that drifted from its sealed digest must not validate")
	}
}

func TestNewAttemptRequiresFencingAndCanonicalIdentity(t *testing.T) {
	spec := testStage(t, testID(t, "stage"), "fetch")
	run, job, ticket := testID(t, "run"), testID(t, "job"), testID(t, "ticket")
	cases := map[string]func() error{
		"zero fence": func() error {
			_, err := NewAttempt(run, job, spec, 1, 0, ticket, "cpu", testStart)
			return err
		},
		"zero attempt": func() error {
			_, err := NewAttempt(run, job, spec, 0, 1, ticket, "cpu", testStart)
			return err
		},
		"run id is not a run": func() error {
			_, err := NewAttempt(job, job, spec, 1, 1, ticket, "cpu", testStart)
			return err
		},
		"ticket id is not a ticket": func() error {
			_, err := NewAttempt(run, job, spec, 1, 1, run, "cpu", testStart)
			return err
		},
		"missing execution class": func() error {
			_, err := NewAttempt(run, job, spec, 1, 1, ticket, "", testStart)
			return err
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			if err := build(); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestTransitionFollowsTheWorkerProtocol(t *testing.T) {
	record, _ := testAttempt(t, 1, 7)
	next, err := record.Transition(AttemptStarting, testStart.Add(time.Second))
	if err != nil {
		t.Fatalf("starting: %v", err)
	}
	if next.Version.Generation() != 2 {
		t.Fatalf("generation = %d, want 2", next.Version.Generation())
	}
	// Leased is not reachable from Starting; only Ready is.
	if _, err := next.Transition(AttemptLeased, testStart.Add(2*time.Second)); err == nil {
		t.Fatal("starting must not reach leased directly")
	}
}

// At-least-once status delivery means the same transition arrives twice. The
// second must be a no-op, not a conflict, or ordinary redelivery would fail runs.
func TestTransitionToTheCurrentStateIsIdempotent(t *testing.T) {
	record, _ := testAttempt(t, 1, 7)
	started, err := record.Transition(AttemptStarting, testStart.Add(time.Second))
	if err != nil {
		t.Fatalf("starting: %v", err)
	}
	again, err := started.Transition(AttemptStarting, testStart.Add(2*time.Second))
	if err != nil {
		t.Fatalf("repeat transition: %v", err)
	}
	if again.Version.Generation() != started.Version.Generation() {
		t.Fatal("a repeated transition must not bump the version")
	}
}

func TestTerminalAttemptRefusesFurtherTransitions(t *testing.T) {
	record, _ := testAttempt(t, 1, 7)
	failed, err := started(t, record).Fail(faults.New(faults.CodeInternal, "boom",
		faults.WithReason("boom"), faults.WithRetryPolicy(faults.NoRetry())), testStart.Add(2*time.Second))
	if err != nil {
		t.Fatalf("fail: %v", err)
	}
	if failed.State != AttemptFailed {
		t.Fatalf("state = %s, want failed", failed.State)
	}
	if failed.Failure.IsZero() {
		t.Fatal("a failed attempt must carry a failure record")
	}
	if _, err := failed.Transition(AttemptStarting, testStart.Add(2*time.Second)); err == nil {
		t.Fatal("a terminal attempt must refuse transitions")
	}
}

// This is the whole point of fencing: a worker that lost its lease keeps running
// until it notices, and its next status must not overwrite the replacement's.
func TestApplyStatusRejectsAStaleFence(t *testing.T) {
	record, _ := testAttempt(t, 1, 7)
	if _, err := record.ApplyStatus(6, 1, AttemptStarting, testStart.Add(time.Second)); err == nil {
		t.Fatal("a stale fence must be rejected")
	}
	if !faults.IsCode(mustStatusError(t, record), faults.CodeConflict) {
		t.Fatal("a stale fence must be reported as a conflict")
	}
}

func mustStatusError(t *testing.T, record AttemptRecord) error {
	t.Helper()
	_, err := record.ApplyStatus(6, 1, AttemptStarting, testStart.Add(time.Second))
	if err == nil {
		t.Fatal("expected an error")
	}
	return err
}

// A replayed status is a duplicate, not corruption, so it is absorbed rather
// than rejected.
func TestApplyStatusIgnoresANonAdvancingSequence(t *testing.T) {
	record, _ := testAttempt(t, 1, 7)
	first, err := record.ApplyStatus(7, 5, AttemptStarting, testStart.Add(time.Second))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if first.Sequence != 5 {
		t.Fatalf("sequence = %d, want 5", first.Sequence)
	}
	replay, err := first.ApplyStatus(7, 5, AttemptReady, testStart.Add(2*time.Second))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.State != AttemptStarting || replay.Sequence != 5 {
		t.Fatal("a replayed status must be absorbed without changing state")
	}
}

// The budget governs whether to start more work; it never invalidates work that
// already happened. libs/go/coordination/workqueue removed the opposite rule
// after it made over-budget records permanently un-terminable.
func TestAnOverBudgetAttemptRemainsValidButNotRetryable(t *testing.T) {
	spec := testStage(t, testID(t, "stage"), "fetch")
	spec.MaximumAttempts = 2
	record, err := NewAttempt(testID(t, "run"), testID(t, "job"), spec,
		5, 9, testID(t, "ticket"), "cpu-general", testStart)
	if err != nil {
		t.Fatalf("an over-budget attempt must still be recordable: %v", err)
	}
	failed, err := started(t, record).Fail(faults.New(faults.CodeInternal, "boom",
		faults.WithReason("boom"), faults.WithRetryPolicy(faults.NoRetry())), testStart.Add(2*time.Second))
	if err != nil {
		t.Fatalf("an over-budget attempt must still reach a terminal state: %v", err)
	}
	if failed.Retryable(spec) {
		t.Fatal("an over-budget attempt must not be retryable")
	}
}

func TestRetryableRespectsTheStageBudget(t *testing.T) {
	spec := testStage(t, testID(t, "stage"), "fetch")
	spec.MaximumAttempts = 3
	boom := faults.New(faults.CodeInternal, "boom",
		faults.WithReason("boom"), faults.WithRetryPolicy(faults.NoRetry()))

	within, _ := testAttempt(t, 1, 7)
	failedWithin, err := started(t, within).Fail(boom, testStart.Add(2*time.Second))
	if err != nil {
		t.Fatalf("fail: %v", err)
	}
	if !failedWithin.Retryable(spec) {
		t.Fatal("an attempt inside the budget must be retryable")
	}

	// A completed attempt is never retried, and neither is a cancelled one:
	// cancellation is a decision that the work should stop, not a failure.
	completed, err := runToCompletion(t, within)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if completed.Retryable(spec) {
		t.Fatal("a completed attempt must not be retryable")
	}
}

func runToCompletion(t *testing.T, record AttemptRecord) (AttemptRecord, error) {
	t.Helper()
	for index, state := range []AttemptState{
		AttemptStarting, AttemptReady, AttemptLeased, AttemptRunning, AttemptCommitting, AttemptCompleted,
	} {
		at := testStart.Add(time.Duration(index+1) * time.Second)
		next, err := record.Transition(state, at)
		if err != nil {
			return AttemptRecord{}, err
		}
		record = next
	}
	return record, nil
}
