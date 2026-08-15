// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package retry

import (
	"context"
	"strings"
	"time"
	"unicode"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
)

const maximumOperationNameLength = 256

// Operation is invoked for each attempt. It must honor context cancellation.
type Operation func(context.Context, Attempt) error

// Executor applies one immutable policy. It is safe for concurrent use.
type Executor struct {
	policy     Policy
	clock      clock.Clock
	observer   Observer
	classifier Classifier
	random     *lockedRandom
}

// NewExecutor constructs a reusable executor.
func NewExecutor(policy Policy, options ...Option) (*Executor, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	configuration := defaultConfiguration()
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&configuration); err != nil {
			return nil, err
		}
	}
	if nilInterface(configuration.clock) {
		return nil, invalidArgument(ErrNilClock, "retry clock must not be nil", "nil_clock", operationNewExecutor, nil)
	}
	if nilClassifier(configuration.classifier) {
		return nil, invalidArgument(ErrNilClassifier, "retry classifier must not be nil", "nil_classifier", operationNewExecutor, nil)
	}
	if nilRandomSource(configuration.random) {
		return nil, invalidArgument(ErrNilRandomSource, "retry random source must not be nil", "nil_random_source", operationNewExecutor, nil)
	}
	return &Executor{
		policy:     policy,
		clock:      configuration.clock,
		observer:   configuration.observer,
		classifier: configuration.classifier,
		random:     &lockedRandom{source: configuration.random},
	}, nil
}

// Do constructs an executor and executes operation.
func Do(ctx context.Context, policy Policy, operationName string, operation Operation, options ...Option) (Result, error) {
	executor, err := NewExecutor(policy, options...)
	if err != nil {
		return Result{}, err
	}
	return executor.Do(ctx, operationName, operation)
}

// Do executes operation until it succeeds, is rejected as non-retryable, is
// exhausted, or ctx is interrupted.
func (executor *Executor) Do(ctx context.Context, operationName string, operation Operation) (Result, error) {
	if ctx == nil {
		return Result{}, invalidArgument(ErrNilContext, "retry context must not be nil", "nil_context", operationExecute, nil)
	}
	if executor == nil {
		return Result{}, invalidArgument(ErrNilExecutor, "retry executor must not be nil", "nil_executor", operationExecute, nil)
	}
	if operation == nil {
		return Result{}, invalidArgument(ErrNilOperation, "retry operation must not be nil", "nil_operation", operationExecute, nil)
	}
	operationName = strings.TrimSpace(operationName)
	if !validOperationName(operationName) {
		return Result{}, invalidArgument(
			ErrInvalidOperationName,
			"invalid retry operation name",
			"invalid_operation_name",
			operationExecute,
			faults.Fields{"retry_operation": operationName},
		)
	}

	startedAt := executor.clock.Now()
	result := Result{operation: operationName, startedAt: startedAt}
	var lastErr error

	for attemptNumber := 1; ; attemptNumber++ {
		now := executor.clock.Now()
		if cause := contextCause(ctx); cause != nil {
			return executor.interrupted(ctx, &result, lastErr, cause)
		}
		if executor.policy.maxElapsed > 0 && attemptNumber > 1 && now.Sub(startedAt) >= executor.policy.maxElapsed {
			return executor.exhausted(ctx, &result, lastErr)
		}

		attempt := Attempt{number: attemptNumber, startedAt: now}
		result.attempts = attemptNumber
		executor.observe(ctx, Event{
			Kind:        EventAttemptStarted,
			Operation:   operationName,
			Attempt:     attemptNumber,
			MaxAttempts: executor.policy.maxAttempts,
			At:          now,
		})

		err := operation(ctx, attempt)
		finishedAttemptAt := executor.clock.Now()
		duration := nonNegativeDuration(finishedAttemptAt.Sub(now))
		if err == nil {
			result.finishedAt = finishedAttemptAt
			result.outcome = OutcomeSucceeded
			result.lastErr = nil
			executor.observe(ctx, Event{
				Kind:        EventSucceeded,
				Operation:   operationName,
				Attempt:     attemptNumber,
				MaxAttempts: executor.policy.maxAttempts,
				At:          finishedAttemptAt,
				Duration:    duration,
				Outcome:     OutcomeSucceeded,
			})
			return result, nil
		}

		lastErr = err
		result.lastErr = err
		executor.observe(ctx, Event{
			Kind:        EventAttemptFailed,
			Operation:   operationName,
			Attempt:     attemptNumber,
			MaxAttempts: executor.policy.maxAttempts,
			At:          finishedAttemptAt,
			Duration:    duration,
			Err:         err,
		})

		if cause := contextCause(ctx); cause != nil {
			return executor.interrupted(ctx, &result, lastErr, cause)
		}

		retryable, limit, delay := executor.decision(err, attemptNumber)
		if !retryable {
			result.finishedAt = finishedAttemptAt
			result.outcome = OutcomeStopped
			executor.observe(ctx, Event{
				Kind:        EventStopped,
				Operation:   operationName,
				Attempt:     attemptNumber,
				MaxAttempts: limit,
				At:          finishedAttemptAt,
				Duration:    duration,
				Outcome:     OutcomeStopped,
				Err:         err,
			})
			return result, err
		}
		if attemptNumber >= limit {
			return executor.exhausted(ctx, &result, lastErr)
		}

		elapsed := nonNegativeDuration(finishedAttemptAt.Sub(startedAt))
		if executor.policy.maxElapsed > 0 && exceedsElapsedBudget(elapsed, delay, executor.policy.maxElapsed) {
			return executor.exhausted(ctx, &result, lastErr)
		}

		result.lastDelay = delay
		executor.observe(ctx, Event{
			Kind:        EventRetryScheduled,
			Operation:   operationName,
			Attempt:     attemptNumber,
			MaxAttempts: limit,
			At:          finishedAttemptAt,
			Duration:    duration,
			Delay:       delay,
			Err:         err,
		})

		if sleepErr := executor.clock.Sleep(ctx, delay); sleepErr != nil {
			return executor.interrupted(ctx, &result, lastErr, sleepErr)
		}
	}
}

func (executor *Executor) decision(err error, attemptNumber int) (retryable bool, limit int, delay time.Duration) {
	limit = executor.policy.maxAttempts
	faultPolicy := faults.RetryPolicyOf(err).Normalized()
	if faultPolicy.MaxAttempts > 0 && faultPolicy.MaxAttempts < limit {
		limit = faultPolicy.MaxAttempts
	}

	switch faultPolicy.Kind {
	case faults.RetryKindNever:
		return false, limit, 0
	case faults.RetryKindImmediate:
		return true, limit, 0
	case faults.RetryKindAfter:
		base := executor.jitteredDelay(attemptNumber)
		if faultPolicy.After > base {
			base = faultPolicy.After
		}
		return true, limit, base
	case faults.RetryKindBackoff:
		return true, limit, executor.jitteredDelay(attemptNumber)
	case faults.RetryKindUnspecified:
		if !executor.classifier.Retryable(err) {
			return false, limit, 0
		}
		return true, limit, executor.jitteredDelay(attemptNumber)
	default:
		return false, limit, 0
	}
}

func (executor *Executor) jitteredDelay(attemptNumber int) time.Duration {
	base := executor.policy.backoff.Delay(attemptNumber)
	if base < 0 {
		base = 0
	}
	return applyJitter(base, executor.policy.jitterFraction, executor.random.sample())
}

func (executor *Executor) exhausted(ctx context.Context, result *Result, lastErr error) (Result, error) {
	result.finishedAt = executor.clock.Now()
	result.outcome = OutcomeExhausted
	result.lastErr = lastErr
	err := exhaustedError(ctx, result.operation, result.attempts, result.Elapsed(), lastErr)
	executor.observe(ctx, Event{
		Kind:        EventStopped,
		Operation:   result.operation,
		Attempt:     result.attempts,
		MaxAttempts: executor.policy.maxAttempts,
		At:          result.finishedAt,
		Outcome:     OutcomeExhausted,
		Err:         err,
	})
	return *result, err
}

func (executor *Executor) interrupted(ctx context.Context, result *Result, lastErr, cause error) (Result, error) {
	result.finishedAt = executor.clock.Now()
	result.outcome = OutcomeInterrupted
	result.lastErr = lastErr
	err := interruptedError(ctx, result.operation, result.attempts, result.Elapsed(), cause, lastErr)
	executor.observe(ctx, Event{
		Kind:        EventStopped,
		Operation:   result.operation,
		Attempt:     result.attempts,
		MaxAttempts: executor.policy.maxAttempts,
		At:          result.finishedAt,
		Outcome:     OutcomeInterrupted,
		Err:         err,
	})
	return *result, err
}

func (executor *Executor) observe(ctx context.Context, event Event) {
	safeObserve(ctx, executor.observer, event)
}

func validOperationName(value string) bool {
	if value == "" || len(value) > maximumOperationNameLength {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return false
		}
	}
	return true
}

func contextCause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		return ctx.Err()
	default:
		return nil
	}
}

func nonNegativeDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}

func exceedsElapsedBudget(elapsed, delay, maximum time.Duration) bool {
	if maximum <= 0 {
		return false
	}
	if elapsed >= maximum {
		return true
	}
	return delay > maximum-elapsed
}
