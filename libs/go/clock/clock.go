// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package clock

import (
	"context"
	"time"
)

// Timer is the subset of time.Timer behavior needed by Mindclade libraries.
//
// C returns the timer's delivery channel. Stop and Reset follow the modern
// Go timer contract: after either method returns, a subsequently received
// value will not be a stale value from the prior timer configuration.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(time.Duration) bool
}

// Ticker is the subset of time.Ticker behavior needed by Mindclade libraries.
// NewTicker and Reset panic when given a non-positive duration, matching the
// standard library.
type Ticker interface {
	C() <-chan time.Time
	Stop()
	Reset(time.Duration)
}

// Clock abstracts wall-clock access and timer construction.
//
// Implementations must be safe for concurrent use. Sleep returns ctx.Err when
// cancellation wins and returns nil after the requested duration elapses.
type Clock interface {
	Now() time.Time
	Since(time.Time) time.Duration
	Until(time.Time) time.Duration
	After(time.Duration) <-chan time.Time
	NewTimer(time.Duration) Timer
	NewTicker(time.Duration) Ticker
	Sleep(context.Context, time.Duration) error
}

func sleep(ctx context.Context, clock Clock, duration time.Duration) error {
	if ctx == nil {
		return ErrNilContext
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if duration <= 0 {
		return nil
	}

	timer := clock.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C():
		return nil
	}
}
