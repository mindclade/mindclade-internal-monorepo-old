// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package retry

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
)

func immediatePolicy(t *testing.T, attempts int) Policy {
	t.Helper()
	policy, err := NewPolicy(
		WithMaxAttempts(attempts),
		WithBackoff(ImmediateBackoff()),
		WithoutJitter(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestExecutorSuccessFirstAttempt(t *testing.T) {
	t.Parallel()

	fake := clock.NewFake(time.Unix(100, 0).UTC())
	executor, err := NewExecutor(immediatePolicy(t, 3), WithClock(fake))
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Do(context.Background(), "model.load", func(_ context.Context, attempt Attempt) error {
		if attempt.Number() != 1 || !attempt.First() || attempt.RetryNumber() != 0 {
			t.Fatalf("unexpected attempt: %+v", attempt)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !result.Succeeded() || result.Attempts() != 1 || result.Outcome() != OutcomeSucceeded || result.Elapsed() != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestExecutorRetriesExplicitFaultAndExhausts(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(immediatePolicy(t, 3))
	if err != nil {
		t.Fatal(err)
	}
	cause := faults.New(
		faults.CodeUnavailable,
		"dependency unavailable",
		faults.WithReason("dependency_unavailable"),
		faults.WithRetryPolicy(faults.ImmediateRetry(0)),
	)
	var calls int
	result, err := executor.Do(context.Background(), "dependency.call", func(context.Context, Attempt) error {
		calls++
		return cause
	})
	if !errors.Is(err, ErrExhausted) || !errors.Is(err, cause) {
		t.Fatalf("error = %v", err)
	}
	if faults.CodeOf(err) != faults.CodeUnavailable || faults.ReasonOf(err) != "retry_exhausted" {
		t.Fatalf("classification = %s/%s", faults.CodeOf(err), faults.ReasonOf(err))
	}
	if calls != 3 || result.Attempts() != 3 || result.Outcome() != OutcomeExhausted {
		t.Fatalf("calls/result = %d/%+v", calls, result)
	}
}

func TestExecutorHonorsFaultAttemptLimit(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(immediatePolicy(t, 10))
	if err != nil {
		t.Fatal(err)
	}
	retryErr := faults.New(
		faults.CodeUnavailable,
		"temporarily unavailable",
		faults.WithRetryPolicy(faults.ImmediateRetry(2)),
	)
	var calls int
	result, err := executor.Do(context.Background(), "limited", func(context.Context, Attempt) error {
		calls++
		return retryErr
	})
	if !errors.Is(err, ErrExhausted) || calls != 2 || result.Attempts() != 2 {
		t.Fatalf("error/calls/result = %v/%d/%+v", err, calls, result)
	}
}

func TestExecutorExplicitNeverOverridesClassifier(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(
		immediatePolicy(t, 5),
		WithClassifier(ClassifierFunc(func(error) bool { return true })),
	)
	if err != nil {
		t.Fatal(err)
	}
	stopErr := faults.New(
		faults.CodeUnavailable,
		"do not retry",
		faults.WithRetryPolicy(faults.NoRetry()),
	)
	var calls int
	result, err := executor.Do(context.Background(), "never", func(context.Context, Attempt) error {
		calls++
		return stopErr
	})
	if !errors.Is(err, stopErr) || errors.Is(err, ErrExhausted) || calls != 1 || result.Outcome() != OutcomeStopped {
		t.Fatalf("error/calls/result = %v/%d/%+v", err, calls, result)
	}
}

func TestExecutorCustomClassifier(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("transient")
	executor, err := NewExecutor(
		immediatePolicy(t, 3),
		WithClassifier(ClassifierFunc(func(err error) bool { return errors.Is(err, sentinel) })),
	)
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	result, err := executor.Do(context.Background(), "classified", func(context.Context, Attempt) error {
		calls++
		if calls < 3 {
			return sentinel
		}
		return nil
	})
	if err != nil || calls != 3 || result.Attempts() != 3 || !result.Succeeded() {
		t.Fatalf("error/calls/result = %v/%d/%+v", err, calls, result)
	}
}

func TestExecutorBackoffWithFakeClock(t *testing.T) {
	t.Parallel()

	fake := clock.NewFake(time.Unix(0, 0).UTC())
	backoff, err := FixedBackoff(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewPolicy(WithMaxAttempts(3), WithBackoff(backoff), WithoutJitter())
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewExecutor(policy, WithClock(fake))
	if err != nil {
		t.Fatal(err)
	}

	retryErr := faults.New(faults.CodeUnavailable, "retry", faults.WithRetryPolicy(faults.BackoffRetry(0)))
	var calls atomic.Int32
	done := make(chan struct{})
	var result Result
	var runErr error
	go func() {
		defer close(done)
		result, runErr = executor.Do(context.Background(), "fake.backoff", func(context.Context, Attempt) error {
			if calls.Add(1) < 3 {
				return retryErr
			}
			return nil
		})
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fake.BlockUntil(waitCtx, 1); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls before advance = %d", calls.Load())
	}
	if err := fake.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if err := fake.BlockUntil(waitCtx, 1); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls after first advance = %d", calls.Load())
	}
	if err := fake.Advance(time.Second); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-waitCtx.Done():
		t.Fatal("execution did not finish")
	}
	if runErr != nil || calls.Load() != 3 || result.Elapsed() != 2*time.Second || result.LastDelay() != time.Second {
		t.Fatalf("error/calls/result = %v/%d/%+v", runErr, calls.Load(), result)
	}
}

func TestExecutorDelayedRetryIsMinimum(t *testing.T) {
	t.Parallel()

	fake := clock.NewFake(time.Unix(0, 0).UTC())
	backoff, _ := FixedBackoff(time.Second)
	policy, _ := NewPolicy(WithMaxAttempts(2), WithBackoff(backoff), WithoutJitter())
	executor, err := NewExecutor(policy, WithClock(fake))
	if err != nil {
		t.Fatal(err)
	}
	delayed := faults.New(
		faults.CodeResourceExhausted,
		"rate limited",
		faults.WithRetryPolicy(faults.DelayedRetry(3*time.Second, 2)),
	)

	done := make(chan error, 1)
	var calls atomic.Int32
	go func() {
		_, err := executor.Do(context.Background(), "rate.limit", func(context.Context, Attempt) error {
			if calls.Add(1) == 1 {
				return delayed
			}
			return nil
		})
		done <- err
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fake.BlockUntil(waitCtx, 1); err != nil {
		t.Fatal(err)
	}
	deadline, ok := fake.NextDeadline()
	if !ok || !deadline.Equal(time.Unix(3, 0).UTC()) {
		t.Fatalf("deadline = %v/%v", deadline, ok)
	}
	if err := fake.Advance(3 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Do: %v", err)
	}
}

func TestExecutorMaxElapsedPreventsLateAttempt(t *testing.T) {
	t.Parallel()

	fake := clock.NewFake(time.Unix(0, 0).UTC())
	backoff, _ := FixedBackoff(2 * time.Second)
	policy, _ := NewPolicy(
		WithMaxAttempts(5),
		WithMaxElapsed(time.Second),
		WithBackoff(backoff),
		WithoutJitter(),
	)
	executor, err := NewExecutor(policy, WithClock(fake))
	if err != nil {
		t.Fatal(err)
	}
	retryErr := faults.New(faults.CodeUnavailable, "retry", faults.WithRetryPolicy(faults.BackoffRetry(0)))
	var calls int
	result, err := executor.Do(context.Background(), "elapsed", func(context.Context, Attempt) error {
		calls++
		return retryErr
	})
	if !errors.Is(err, ErrExhausted) || calls != 1 || result.Attempts() != 1 {
		t.Fatalf("error/calls/result = %v/%d/%+v", err, calls, result)
	}
}

func TestExecutorCancellationDuringDelay(t *testing.T) {
	t.Parallel()

	fake := clock.NewFake(time.Unix(0, 0).UTC())
	backoff, _ := FixedBackoff(time.Hour)
	policy, _ := NewPolicy(WithMaxAttempts(3), WithBackoff(backoff), WithoutJitter())
	executor, err := NewExecutor(policy, WithClock(fake))
	if err != nil {
		t.Fatal(err)
	}
	retryErr := faults.New(faults.CodeUnavailable, "retry", faults.WithRetryPolicy(faults.BackoffRetry(0)))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var result Result
	var runErr error
	go func() {
		defer close(done)
		result, runErr = executor.Do(ctx, "cancel", func(context.Context, Attempt) error { return retryErr })
	}()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	if err := fake.BlockUntil(waitCtx, 1); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-done:
	case <-waitCtx.Done():
		t.Fatal("execution did not cancel")
	}
	if !errors.Is(runErr, ErrInterrupted) || !errors.Is(runErr, context.Canceled) || faults.CodeOf(runErr) != faults.CodeCanceled {
		t.Fatalf("error = %v", runErr)
	}
	if result.Outcome() != OutcomeInterrupted || result.Attempts() != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestExecutorObserverEvents(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var kinds []EventKind
	observer := ObserverFunc(func(_ context.Context, event Event) {
		mu.Lock()
		kinds = append(kinds, event.Kind)
		mu.Unlock()
	})
	executor, err := NewExecutor(immediatePolicy(t, 2), WithObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	retryErr := faults.New(faults.CodeUnavailable, "retry", faults.WithRetryPolicy(faults.ImmediateRetry(2)))
	calls := 0
	_, err = executor.Do(context.Background(), "events", func(context.Context, Attempt) error {
		calls++
		if calls == 1 {
			return retryErr
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]EventKind(nil), kinds...)
	mu.Unlock()
	want := []EventKind{EventAttemptStarted, EventAttemptFailed, EventRetryScheduled, EventAttemptStarted, EventSucceeded}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestExecutorConcurrentUse(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(immediatePolicy(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	retryErr := faults.New(faults.CodeUnavailable, "retry", faults.WithRetryPolicy(faults.ImmediateRetry(2)))
	const workers = 32
	var wg sync.WaitGroup
	wg.Add(workers)
	errorsCh := make(chan error, workers)
	for range workers {
		go func() {
			defer wg.Done()
			calls := 0
			result, err := executor.Do(context.Background(), "concurrent", func(context.Context, Attempt) error {
				calls++
				if calls == 1 {
					return retryErr
				}
				return nil
			})
			if err != nil || !result.Succeeded() || result.Attempts() != 2 {
				errorsCh <- errors.New("unexpected concurrent result")
			}
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
}
