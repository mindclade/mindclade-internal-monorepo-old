// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package retry

import (
	"context"
	"runtime/debug"
	"time"

	"mindclade.internal/libs/go/faults"
)

// EventKind identifies a retry lifecycle event.
type EventKind string

const (
	EventAttemptStarted EventKind = "attempt_started"
	EventAttemptFailed  EventKind = "attempt_failed"
	EventRetryScheduled EventKind = "retry_scheduled"
	EventSucceeded      EventKind = "succeeded"
	EventStopped        EventKind = "stopped"
)

// Event is an immutable best-effort diagnostics record.
type Event struct {
	Kind        EventKind
	Operation   string
	Attempt     int
	MaxAttempts int
	At          time.Time
	Duration    time.Duration
	Delay       time.Duration
	Outcome     Outcome
	Err         error
}

// Fields returns bounded structured fields suitable for logging or metrics.
func (event Event) Fields() faults.Fields {
	fields := faults.Fields{
		"event_kind":      string(event.Kind),
		"retry_operation": event.Operation,
		"attempt":         event.Attempt,
		"max_attempts":    event.MaxAttempts,
	}
	if event.Duration > 0 {
		fields["attempt_duration"] = event.Duration.String()
	}
	if event.Delay > 0 {
		fields["retry_delay"] = event.Delay.String()
	}
	if event.Outcome != "" {
		fields["retry_outcome"] = string(event.Outcome)
	}
	if event.Err != nil {
		fields["error_code"] = faults.CodeOf(event.Err).String()
		if reason := faults.ReasonOf(event.Err); reason != "" {
			fields["error_reason"] = reason
		}
	}
	return fields.Clone()
}

// Observer consumes retry lifecycle events. Implementations should be safe for
// concurrent use and must not block operation progress indefinitely.
type Observer interface {
	Observe(context.Context, Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(context.Context, Event)

func (function ObserverFunc) Observe(ctx context.Context, event Event) {
	if function != nil {
		function(ctx, event)
	}
}

// CombineObservers returns an observer that invokes each non-nil observer in
// order. Each observer is panic-isolated.
func CombineObservers(observers ...Observer) Observer {
	captured := make([]Observer, 0, len(observers))
	for _, observer := range observers {
		if !nilObserver(observer) {
			captured = append(captured, observer)
		}
	}
	if len(captured) == 0 {
		return nil
	}
	return ObserverFunc(func(ctx context.Context, event Event) {
		for _, observer := range captured {
			safeObserve(ctx, observer, event)
		}
	})
}

func safeObserve(ctx context.Context, observer Observer, event Event) {
	if nilObserver(observer) {
		return
	}
	defer func() {
		if recover() != nil {
			_ = debug.Stack()
		}
	}()
	observer.Observe(ctx, event)
}
