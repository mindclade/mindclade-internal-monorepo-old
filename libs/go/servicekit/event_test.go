// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package servicekit

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"mindclade.internal/libs/go/faults"
)

func TestEventFieldsAndClassification(t *testing.T) {
	t.Parallel()

	err := faults.New(
		faults.CodeUnavailable,
		"worker unavailable",
		faults.WithReason("worker_unavailable"),
		faults.WithField("worker_pool", "fold"),
	)
	event := Event{
		Kind:      EventComponentExited,
		Service:   "control-plane",
		Component: "worker",
		At:        time.Date(2026, 8, 12, 16, 30, 0, 0, time.UTC),
		Duration:  1250 * time.Millisecond,
		Err:       err,
	}
	if event.ErrorCode() != faults.CodeUnavailable {
		t.Fatalf("ErrorCode() = %s", event.ErrorCode())
	}
	fields := event.Fields()
	if fields[FieldEventKind] != string(EventComponentExited) ||
		fields[FieldServiceName] != "control-plane" ||
		fields[FieldComponentName] != "worker" ||
		fields["worker_pool"] != "fold" ||
		fields["duration_ms"] != int64(1250) {
		t.Fatalf("unexpected fields: %#v", fields)
	}
	if _, exists := fields["state_from"]; exists {
		t.Fatalf("component event unexpectedly contains state fields: %#v", fields)
	}
	fields[FieldServiceName] = "mutated"
	if event.Fields()[FieldServiceName] != "control-plane" {
		t.Fatal("Event.Fields returned shared state")
	}
}

func TestStateChangedEventFields(t *testing.T) {
	t.Parallel()

	event := Event{
		Kind:    EventStateChanged,
		Service: "api",
		From:    StateStarting,
		To:      StateRunning,
	}
	fields := event.Fields()
	if fields["state_from"] != "starting" || fields["state_to"] != "running" {
		t.Fatalf("unexpected transition fields: %#v", fields)
	}
}

func TestCombineObserversIsolatesPanicsAndPreservesOrder(t *testing.T) {
	t.Parallel()

	var received []string
	observer := CombineObservers(
		ObserverFunc(func(Event) { received = append(received, "first") }),
		ObserverFunc(func(Event) { panic("observer failure") }),
		nil,
		ObserverFunc(func(Event) { received = append(received, "third") }),
	)
	if observer == nil {
		t.Fatal("CombineObservers returned nil")
	}
	observer.Observe(Event{Kind: EventStateChanged})
	if !reflect.DeepEqual(received, []string{"first", "third"}) {
		t.Fatalf("observer order = %v", received)
	}
	if CombineObservers(nil, nil) != nil {
		t.Fatal("empty CombineObservers should return nil")
	}
}

func TestSafeObserveReturnsPanicError(t *testing.T) {
	t.Parallel()

	panicErr := safeObserve(ObserverFunc(func(Event) { panic("boom") }), Event{})
	if panicErr == nil || faults.CodeOf(panicErr) != faults.CodeInternal {
		t.Fatalf("safeObserve = %v", panicErr)
	}
	if !errors.As(panicErr, new(*PanicError)) {
		t.Fatalf("safeObserve returned %T", panicErr)
	}
}
