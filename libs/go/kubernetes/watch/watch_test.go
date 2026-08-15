// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package watch

import (
	"context"
	"errors"
	"testing"

	"mindclade.internal/libs/go/faults"

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
