// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"runtime/debug"
	"time"

	"mindclade.internal/libs/go/faults"
)

// EventKind is the stable category of a lifecycle event.
type EventKind string

const (
	EventStateChanged      EventKind = "state_changed"
	EventComponentStarting EventKind = "component_starting"
	EventComponentStarted  EventKind = "component_started"
	EventComponentRunning  EventKind = "component_running"
	EventComponentExited   EventKind = "component_exited"
	EventComponentDraining EventKind = "component_draining"
	EventComponentDrained  EventKind = "component_drained"
	EventComponentStopping EventKind = "component_stopping"
	EventComponentStopped  EventKind = "component_stopped"
)

// Event is an immutable lifecycle observation suitable for adaptation into
// structured logs, traces, metrics, or audit diagnostics.
type Event struct {
	Kind      EventKind
	Service   string
	Component string
	From      State
	To        State
	At        time.Time
	Duration  time.Duration
	Err       error
}

// ErrorCode returns the event failure's transport-neutral classification.
func (event Event) ErrorCode() faults.Code {
	return faults.CodeOf(event.Err)
}

// Fields returns a fresh set of stable structured attributes. It merges
// lifecycle attributes over any fields carried by Event.Err.
func (event Event) Fields() faults.Fields {
	fields := faults.FieldsOf(event.Err).Merge(faults.Fields{
		FieldEventKind:   string(event.Kind),
		FieldServiceName: event.Service,
	})
	if event.Component != "" {
		fields[FieldComponentName] = event.Component
	}
	if event.Kind == EventStateChanged {
		fields["state_from"] = event.From.String()
		fields["state_to"] = event.To.String()
	}
	if !event.At.IsZero() {
		fields["event_time"] = event.At.UTC().Format(time.RFC3339Nano)
	}
	if event.Duration > 0 {
		fields["duration_ms"] = event.Duration.Milliseconds()
	}
	return fields.Clone()
}

// Observer receives lifecycle events. Observe must be fast and non-blocking.
// Panics are contained so an observability adapter cannot crash a service.
type Observer interface {
	Observe(Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(Event)

// Observe implements Observer.
func (function ObserverFunc) Observe(event Event) {
	if function != nil {
		function(event)
	}
}

// CombineObservers creates a deterministic fan-out observer. Nil observers are
// ignored. Each observer is panic-isolated so one broken adapter does not
// prevent later adapters from receiving the event.
func CombineObservers(observers ...Observer) Observer {
	captured := make([]Observer, 0, len(observers))
	for _, observer := range observers {
		if observer != nil {
			captured = append(captured, observer)
		}
	}
	if len(captured) == 0 {
		return nil
	}
	return ObserverFunc(func(event Event) {
		for _, observer := range captured {
			safeObserve(observer, event)
		}
	})
}

func safeObserve(observer Observer, event Event) (panicErr *PanicError) {
	if observer == nil {
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			panicErr = &PanicError{Value: recovered, stack: debug.Stack()}
		}
	}()
	observer.Observe(event)
	return nil
}
