// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package clock

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRealClockNowAndDurations(t *testing.T) {
	t.Parallel()

	clock := RealClock{}
	before := time.Now()
	now := clock.Now()
	after := time.Now()
	if now.Before(before) || now.After(after) {
		t.Fatalf("Now() = %v, outside [%v, %v]", now, before, after)
	}

	past := now.Add(-time.Second)
	future := now.Add(time.Second)
	if elapsed := clock.Since(past); elapsed < 900*time.Millisecond {
		t.Fatalf("Since() = %v", elapsed)
	}
	if remaining := clock.Until(future); remaining <= 0 {
		t.Fatalf("Until() = %v", remaining)
	}
}

func TestSleepRejectsNilAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	clock := RealClock{}
	if err := clock.Sleep(nil, time.Second); !errors.Is(err, ErrNilContext) {
		t.Fatalf("Sleep(nil) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := clock.Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep(canceled) error = %v", err)
	}

	if err := clock.Sleep(context.Background(), 0); err != nil {
		t.Fatalf("Sleep(0) error = %v", err)
	}
}

func TestRealTimerAndTickerInterfaces(t *testing.T) {
	t.Parallel()

	timer := RealClock{}.NewTimer(time.Hour)
	if !timer.Stop() {
		t.Fatal("new timer was not active")
	}

	ticker := RealClock{}.NewTicker(time.Hour)
	ticker.Reset(2 * time.Hour)
	ticker.Stop()
}
