// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package watch

import (
	"context"
	"errors"
	"testing"

	"go.mindclade.dev/libs/go/faults"

	k8swatch "k8s.io/apimachinery/pkg/watch"
)

type fakeWatcher struct {
	channel chan k8swatch.Event
	stopped bool
}

func (watcher *fakeWatcher) Stop()                             { watcher.stopped = true }
func (watcher *fakeWatcher) ResultChan() <-chan k8swatch.Event { return watcher.channel }

func TestConsumeClosure(t *testing.T) {
	watcher := &fakeWatcher{channel: make(chan k8swatch.Event)}
	close(watcher.channel)
	err := Consume(context.Background(), watcher, Options{}, func(context.Context, k8swatch.Event) error { return nil })
	if !errors.Is(err, ErrClosed) || !watcher.stopped {
		t.Fatalf("Consume() = %v, stopped=%v", err, watcher.stopped)
	}
}

func TestUntil(t *testing.T) {
	watcher := &fakeWatcher{channel: make(chan k8swatch.Event, 1)}
	watcher.channel <- k8swatch.Event{Type: k8swatch.Added}
	event, err := Until(context.Background(), watcher, Options{}, func(event k8swatch.Event) (bool, error) { return event.Type == k8swatch.Added, nil })
	if err != nil || event.Type != k8swatch.Added || !watcher.stopped {
		t.Fatalf("Until() = (%#v, %v), stopped=%v", event, err, watcher.stopped)
	}
}

func TestConsumeContainsHandlerPanic(t *testing.T) {
	watcher := &fakeWatcher{channel: make(chan k8swatch.Event, 1)}
	watcher.channel <- k8swatch.Event{Type: k8swatch.Added}
	err := Consume(context.Background(), watcher, Options{}, func(context.Context, k8swatch.Event) error {
		panic("boom")
	})
	if !faults.IsCode(err, faults.CodeInternal) || !watcher.stopped {
		t.Fatalf("Consume() = %v, stopped=%v", err, watcher.stopped)
	}
}

// AllowCleanClosure lets Consume treat a closed result channel as success.
// Until used to forward that nil straight to the caller together with a zero
// Event, which is indistinguishable from a genuine match on a zero-valued
// event — a silent false success on every "wait until" call site.
func TestUntilRejectsCleanClosureWithoutMatch(t *testing.T) {
	watcher := &fakeWatcher{channel: make(chan k8swatch.Event)}
	close(watcher.channel)
	event, err := Until(context.Background(), watcher, Options{AllowCleanClosure: true},
		func(k8swatch.Event) (bool, error) { return false, nil })
	if err == nil {
		t.Fatalf("Until() = (%#v, nil): a closed watch with no match must not report success", event)
	}
	if !faults.IsCode(err, faults.CodeUnavailable) || faults.ReasonOf(err) != "watch_ended_without_match" {
		t.Fatalf("Until() = %v", err)
	}
	if !watcher.stopped {
		t.Fatal("Until() did not stop the watcher")
	}
}

// The stream ending before a match is one outcome, so it must carry one reason.
// AllowCleanClosure only decides whether Consume calls a closed channel a
// success; letting it also decide which of two fault reasons Until reports
// would make a caller that buckets watch failures by faults.ReasonOf split one
// condition across two buckets.
func TestUntilReportsOneReasonForEveryClosure(t *testing.T) {
	for _, allowCleanClosure := range []bool{false, true} {
		watcher := &fakeWatcher{channel: make(chan k8swatch.Event)}
		close(watcher.channel)
		_, err := Until(context.Background(), watcher, Options{AllowCleanClosure: allowCleanClosure},
			func(k8swatch.Event) (bool, error) { return false, nil })
		if faults.ReasonOf(err) != "watch_ended_without_match" {
			t.Fatalf("Until(AllowCleanClosure=%v) reason = %q (%v)", allowCleanClosure, faults.ReasonOf(err), err)
		}
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("Until(AllowCleanClosure=%v) = %v, want an ErrClosed cause", allowCleanClosure, err)
		}
	}
}
