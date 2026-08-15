// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"context"
	"errors"
	"sync"
	"time"

	"mindclade.internal/libs/go/clock"
)

// deadlineContext preserves normal context deadline semantics while allowing
// the deadline timer to be driven by an injected clock. context.WithCancelCause
// reports context.Canceled from Err even when the cause is DeadlineExceeded;
// this wrapper restores the expected Err value for timeout consumers.
type deadlineContext struct {
	context.Context
	deadline time.Time
}

func (ctx *deadlineContext) Deadline() (time.Time, bool) {
	if ctx == nil {
		return time.Time{}, false
	}
	if parentDeadline, ok := ctx.Context.Deadline(); ok && parentDeadline.Before(ctx.deadline) {
		return parentDeadline, true
	}
	return ctx.deadline, true
}

func (ctx *deadlineContext) Err() error {
	if ctx == nil || ctx.Context.Err() == nil {
		return nil
	}
	if errors.Is(context.Cause(ctx.Context), context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return ctx.Context.Err()
}

func withClockTimeout(parent context.Context, valueClock clock.Clock, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		ctx, cancelCause := context.WithCancelCause(parent)
		cancelCause(context.DeadlineExceeded)
		return &deadlineContext{Context: ctx, deadline: valueClock.Now()}, func() {}
	}

	ctx, cancelCause := context.WithCancelCause(parent)
	wrapped := &deadlineContext{Context: ctx, deadline: valueClock.Now().Add(timeout)}
	timer := valueClock.NewTimer(timeout)
	var once sync.Once
	stop := func(cause error) {
		once.Do(func() {
			timer.Stop()
			cancelCause(cause)
		})
	}

	go func() {
		select {
		case <-timer.C():
			stop(context.DeadlineExceeded)
		case <-ctx.Done():
			stop(context.Cause(ctx))
		}
	}()

	return wrapped, func() { stop(context.Canceled) }
}
