// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package clock

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestFakeClockTimerAdvance(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	clock := NewFake(start)
	timer := clock.NewTimer(10 * time.Second)

	if got := clock.Pending(); got != 1 {
		t.Fatalf("Pending() = %d, want 1", got)
	}
	if deadline, ok := clock.NextDeadline(); !ok || !deadline.Equal(start.Add(10*time.Second)) {
		t.Fatalf("NextDeadline() = %v, %v", deadline, ok)
	}

	if err := clock.Advance(9 * time.Second); err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	select {
	case value := <-timer.C():
		t.Fatalf("timer fired early at %v", value)
	default:
	}

	if err := clock.Advance(time.Second); err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	select {
	case value := <-timer.C():
		if !value.Equal(start.Add(10 * time.Second)) {
			t.Fatalf("timer value = %v", value)
		}
	default:
		t.Fatal("timer did not fire")
	}

	if got := clock.Pending(); got != 0 {
		t.Fatalf("Pending() = %d, want 0", got)
	}
}

func TestFakeClockImmediateTimer(t *testing.T) {
	t.Parallel()

	start := time.Unix(100, 0)
	clock := NewFake(start)
	timer := clock.NewTimer(0)

	select {
	case value := <-timer.C():
		if !value.Equal(start) {
			t.Fatalf("timer value = %v, want %v", value, start)
		}
	default:
		t.Fatal("immediate timer did not fire")
	}
	if timer.Stop() {
		t.Fatal("Stop() = true for expired timer")
	}
}

func TestFakeTimerStopAndReset(t *testing.T) {
	t.Parallel()

	start := time.Unix(100, 0)
	clock := NewFake(start)
	timer := clock.NewTimer(time.Minute)
	if !timer.Stop() {
		t.Fatal("Stop() = false for active timer")
	}
	if timer.Stop() {
		t.Fatal("second Stop() = true")
	}

	if wasActive := timer.Reset(30 * time.Second); wasActive {
		t.Fatal("Reset() = true after stopped timer")
	}
	if wasActive := timer.Reset(45 * time.Second); !wasActive {
		t.Fatal("Reset() = false for active timer")
	}

	if err := clock.Advance(44 * time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-timer.C():
		t.Fatal("timer fired before reset deadline")
	default:
	}
	if err := clock.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if got := <-timer.C(); !got.Equal(start.Add(45 * time.Second)) {
		t.Fatalf("timer fired at %v", got)
	}

	if wasActive := timer.Reset(-time.Second); wasActive {
		t.Fatal("Reset() = true for expired timer")
	}
	if got := <-timer.C(); !got.Equal(clock.Now()) {
		t.Fatalf("immediate reset fired at %v, want %v", got, clock.Now())
	}
}

func TestFakeTickerDropsMissedTicksAndResets(t *testing.T) {
	t.Parallel()

	start := time.Unix(0, 0)
	clock := NewFake(start)
	ticker := clock.NewTicker(10 * time.Second)

	if err := clock.Advance(35 * time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-ticker.C():
		if !value.Equal(start.Add(10 * time.Second)) {
			t.Fatalf("first tick = %v", value)
		}
	default:
		t.Fatal("ticker did not fire")
	}
	select {
	case extra := <-ticker.C():
		t.Fatalf("unexpected buffered missed tick %v", extra)
	default:
	}

	deadline, ok := clock.NextDeadline()
	if !ok || !deadline.Equal(start.Add(40*time.Second)) {
		t.Fatalf("next deadline = %v, %v", deadline, ok)
	}

	ticker.Reset(20 * time.Second)
	deadline, ok = clock.NextDeadline()
	if !ok || !deadline.Equal(start.Add(55*time.Second)) {
		t.Fatalf("reset deadline = %v, %v", deadline, ok)
	}

	ticker.Stop()
	if got := clock.Pending(); got != 0 {
		t.Fatalf("Pending() = %d after Stop", got)
	}
}

func TestFakeTickerPanicsForInvalidDuration(t *testing.T) {
	t.Parallel()

	clock := NewFake(time.Time{})
	assertPanics(t, func() { clock.NewTicker(0) })
	ticker := clock.NewTicker(time.Second)
	assertPanics(t, func() { ticker.Reset(0) })
}

func TestFakeClockSetAndAdvanceNext(t *testing.T) {
	t.Parallel()

	start := time.Unix(100, 0)
	clock := NewFake(start)
	clock.NewTimer(5 * time.Second)
	clock.NewTimer(10 * time.Second)

	advanced, ok := clock.AdvanceNext()
	if !ok || !advanced.Equal(start.Add(5*time.Second)) {
		t.Fatalf("AdvanceNext() = %v, %v", advanced, ok)
	}
	if !clock.Now().Equal(start.Add(5 * time.Second)) {
		t.Fatalf("Now() = %v", clock.Now())
	}

	if err := clock.Set(start.Add(20 * time.Second)); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := clock.Set(start); !errors.Is(err, ErrTimeReversal) {
		t.Fatalf("reverse Set() error = %v", err)
	}
	if err := clock.Advance(-time.Second); !errors.Is(err, ErrTimeReversal) {
		t.Fatalf("negative Advance() error = %v", err)
	}
}

func TestFakeClockSleepAndBlockUntil(t *testing.T) {
	t.Parallel()

	clock := NewFake(time.Unix(100, 0))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- clock.Sleep(context.Background(), time.Minute)
	}()

	if err := clock.BlockUntil(ctx, 1); err != nil {
		t.Fatalf("BlockUntil() error = %v", err)
	}
	if err := clock.Advance(time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Sleep() error = %v", err)
	}
}

func TestFakeClockCanceledSleepRemovesTimer(t *testing.T) {
	t.Parallel()

	clock := NewFake(time.Unix(100, 0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- clock.Sleep(ctx, time.Hour)
	}()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := clock.BlockUntil(waitCtx, 1); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep() error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for clock.Pending() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := clock.Pending(); got != 0 {
		t.Fatalf("Pending() = %d after canceled sleep", got)
	}
}

func TestFakeClockConcurrentTimerRegistration(t *testing.T) {
	t.Parallel()

	clock := NewFake(time.Time{})
	const count = 100
	var group sync.WaitGroup
	group.Add(count)
	for index := 0; index < count; index++ {
		go func() {
			defer group.Done()
			clock.NewTimer(time.Hour)
		}()
	}
	group.Wait()
	if got := clock.Pending(); got != count {
		t.Fatalf("Pending() = %d, want %d", got, count)
	}
}

func TestBlockUntilContextValidation(t *testing.T) {
	t.Parallel()

	clock := NewFake(time.Time{})
	if err := clock.BlockUntil(nil, 1); !errors.Is(err, ErrNilContext) {
		t.Fatalf("BlockUntil(nil) error = %v", err)
	}
	if err := clock.BlockUntil(context.Background(), 0); err != nil {
		t.Fatalf("BlockUntil(0) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := clock.BlockUntil(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("BlockUntil(canceled) error = %v", err)
	}
}

func assertPanics(t *testing.T, function func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("function did not panic")
		}
	}()
	function()
}
