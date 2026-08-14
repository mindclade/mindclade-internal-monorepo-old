// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package controller

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
)

// Event is the immutable outcome of one reconciliation attempt.
type Event struct {
	Request    reconcile.Request
	StartedAt  time.Time
	FinishedAt time.Time
	Result     reconcile.Result
	Err        error
}

func (event Event) Duration() time.Duration {
	if event.StartedAt.IsZero() || event.FinishedAt.Before(event.StartedAt) {
		return 0
	}
	return event.FinishedAt.Sub(event.StartedAt)
}

// Observer receives reconciliation lifecycle events. Implementations must be
// safe for concurrent use.
type Observer interface {
	Observe(context.Context, Event)
}

type ObserverFunc func(context.Context, Event)

func (function ObserverFunc) Observe(ctx context.Context, event Event) {
	if function != nil {
		function(ctx, event)
	}
}

// Observe returns middleware that reports reconciliation outcomes. Observer
// panics are contained because telemetry must not alter controller correctness.
func Observe(serviceClock clock.Clock, observer Observer) (Middleware, error) {
	if isNil(serviceClock) {
		return nil, faults.New(
			faults.CodeInvalidArgument,
			"clock is required",
			faults.WithReason("nil_clock"),
			faults.WithOperation("kubernetes.controller.Observe"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if isNil(observer) {
		return nil, faults.New(
			faults.CodeInvalidArgument,
			"controller observer is required",
			faults.WithReason("nil_observer"),
			faults.WithOperation("kubernetes.controller.Observe"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return func(next reconcile.Reconciler) reconcile.Reconciler {
		return ReconcilerFunc(func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
			startedAt := serviceClock.Now().UTC()
			result, err := next.Reconcile(ctx, request)
			event := Event{
				Request:    request,
				StartedAt:  startedAt,
				FinishedAt: serviceClock.Now().UTC(),
				Result:     result,
				Err:        err,
			}
			notify(ctx, observer, event)
			return result, err
		})
	}, nil
}

func notify(ctx context.Context, observer Observer, event Event) {
	defer func() { _ = recover() }()
	observer.Observe(ctx, event)
}
