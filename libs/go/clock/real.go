// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package clock

import (
	"context"
	"time"
)

// RealClock delegates to the Go standard library. Its zero value is ready for
// use and it is safe to copy.
type RealClock struct{}

var _ Clock = RealClock{}

// Now returns the current local time, including the standard library's
// monotonic component when available.
func (RealClock) Now() time.Time {
	return time.Now()
}

// Since returns the elapsed duration since timestamp.
func (clock RealClock) Since(timestamp time.Time) time.Duration {
	return clock.Now().Sub(timestamp)
}

// Until returns the duration until timestamp.
func (clock RealClock) Until(timestamp time.Time) time.Duration {
	return timestamp.Sub(clock.Now())
}

// After waits for duration and then delivers the current time.
func (RealClock) After(duration time.Duration) <-chan time.Time {
	return time.After(duration)
}

// NewTimer creates a standard-library timer.
func (RealClock) NewTimer(duration time.Duration) Timer {
	return realTimer{timer: time.NewTimer(duration)}
}

// NewTicker creates a standard-library ticker. It panics for non-positive
// durations, matching time.NewTicker.
func (RealClock) NewTicker(duration time.Duration) Ticker {
	return realTicker{ticker: time.NewTicker(duration)}
}

// Sleep blocks until duration elapses or ctx is canceled.
func (clock RealClock) Sleep(ctx context.Context, duration time.Duration) error {
	return sleep(ctx, clock, duration)
}

type realTimer struct {
	timer *time.Timer
}

var _ Timer = realTimer{}

func (timer realTimer) C() <-chan time.Time {
	return timer.timer.C
}

func (timer realTimer) Stop() bool {
	return timer.timer.Stop()
}

func (timer realTimer) Reset(duration time.Duration) bool {
	return timer.timer.Reset(duration)
}

type realTicker struct {
	ticker *time.Ticker
}

var _ Ticker = realTicker{}

func (ticker realTicker) C() <-chan time.Time {
	return ticker.ticker.C
}

func (ticker realTicker) Stop() {
	ticker.ticker.Stop()
}

func (ticker realTicker) Reset(duration time.Duration) {
	ticker.ticker.Reset(duration)
}
