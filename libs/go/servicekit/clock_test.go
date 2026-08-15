// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"context"
	"errors"
	"testing"
	"time"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
)

func TestClockDrivenTimeoutContext(t *testing.T) {
	start := time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC)
	fakeClock := clock.NewFake(start)
	ctx, cancel := withClockTimeout(context.Background(), fakeClock, 5*time.Second)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok || !deadline.Equal(start.Add(5*time.Second)) {
		t.Fatalf("Deadline() = %v, %v", deadline, ok)
	}
	if err := fakeClock.Advance(4 * time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		t.Fatal("context canceled before injected deadline")
	default:
	}
	if err := fakeClock.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context did not observe fake-clock deadline")
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) || !errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
		t.Fatalf("Err/Cause = %v/%v", ctx.Err(), context.Cause(ctx))
	}
}

func TestProbeSetWithFakeClockTimeout(t *testing.T) {
	fakeClock := clock.NewFake(time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC))
	set, err := NewProbeSetWithClock(3*time.Second, fakeClock)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Register("blocked", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}

	done := make(chan ProbeReport, 1)
	go func() { done <- set.Check(context.Background()) }()
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := fakeClock.BlockUntil(waitCtx, 1); err != nil {
		t.Fatalf("probe did not register timer: %v", err)
	}
	if err := fakeClock.Advance(3 * time.Second); err != nil {
		t.Fatal(err)
	}
	var report ProbeReport
	select {
	case report = <-done:
	case <-time.After(time.Second):
		t.Fatal("probe check did not finish")
	}
	if report.OK || len(report.Results) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if !errors.Is(report.Results[0].Err, context.DeadlineExceeded) || faults.ReasonOf(report.Results[0].Err) != "probe_timeout" {
		t.Fatalf("probe error = %v", report.Results[0].Err)
	}
	if report.Duration != 3*time.Second || report.Results[0].Duration != 3*time.Second {
		t.Fatalf("durations = %v / %v", report.Duration, report.Results[0].Duration)
	}
}

func TestServiceStartupTimeoutUsesInjectedClock(t *testing.T) {
	fakeClock := clock.NewFake(time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC))
	service, err := New(
		"clocked-startup",
		WithClock(fakeClock),
		WithStartupTimeout(7*time.Second),
		WithShutdownTimeout(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Add(Component{
		Name: "blocked",
		Start: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- service.Run(context.Background()) }()
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := fakeClock.BlockUntil(waitCtx, 1); err != nil {
		t.Fatalf("startup did not register timer: %v", err)
	}
	if err := fakeClock.Advance(7 * time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, ErrStartupTimeout) || !errors.Is(runErr, context.DeadlineExceeded) {
			t.Fatalf("Run() error = %v", runErr)
		}
		if faults.CodeOf(runErr) != faults.CodeDeadlineExceeded {
			t.Fatalf("Run() code = %s", faults.CodeOf(runErr))
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not finish after fake-clock startup timeout")
	}
}

func TestNewProbeSetWithClockValidation(t *testing.T) {
	if _, err := NewProbeSetWithClock(time.Second, nil); !errors.Is(err, ErrNilClock) {
		t.Fatalf("nil clock error = %v", err)
	}
	if _, err := NewProbeSetWithClock(-time.Second, clock.RealClock{}); !errors.Is(err, ErrInvalidDuration) {
		t.Fatalf("negative timeout error = %v", err)
	}
}
