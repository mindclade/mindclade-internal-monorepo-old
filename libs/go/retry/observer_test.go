// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package retry

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

func TestCombineObserversOrderAndPanicIsolation(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var calls []int
	observer := CombineObservers(
		ObserverFunc(func(context.Context, Event) {
			mu.Lock()
			calls = append(calls, 1)
			mu.Unlock()
		}),
		ObserverFunc(func(context.Context, Event) { panic("boom") }),
		nil,
		ObserverFunc(func(context.Context, Event) {
			mu.Lock()
			calls = append(calls, 3)
			mu.Unlock()
		}),
	)
	if observer == nil {
		t.Fatal("observer is nil")
	}
	observer.Observe(context.Background(), Event{Kind: EventAttemptStarted})

	mu.Lock()
	got := append([]int(nil), calls...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []int{1, 3}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestEventFields(t *testing.T) {
	t.Parallel()

	event := Event{
		Kind:        EventRetryScheduled,
		Operation:   "artifact.publish",
		Attempt:     2,
		MaxAttempts: 5,
		Delay:       10,
		Outcome:     OutcomeStopped,
	}
	fields := event.Fields()
	if fields["retry_operation"] != "artifact.publish" || fields["attempt"] != 2 {
		t.Fatalf("fields = %#v", fields)
	}
	fields["attempt"] = 99
	if event.Fields()["attempt"] != 2 {
		t.Fatal("event fields were mutable")
	}
}
