// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package servicekit

import (
	"errors"
	"testing"
	"time"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
)

func TestDurationOptions(t *testing.T) {
	t.Parallel()

	options := []Option{
		WithStartupTimeout(-time.Second),
		WithShutdownTimeout(-time.Second),
		WithComponentDrainTimeout(-time.Second),
		WithComponentStopTimeout(-time.Second),
		WithProbeTimeout(-time.Second),
	}
	for index, option := range options {
		config := defaultConfiguration()
		err := option(&config)
		if !errors.Is(err, ErrInvalidDuration) {
			t.Fatalf("option %d error = %v, want ErrInvalidDuration", index, err)
		}
		if faults.CodeOf(err) != faults.CodeInvalidArgument || faults.ReasonOf(err) != "invalid_duration" {
			t.Fatalf("option %d classification = %s/%q", index, faults.CodeOf(err), faults.ReasonOf(err))
		}
	}

	service, err := New(
		"configured",
		WithStartupTimeout(0),
		WithShutdownTimeout(0),
		WithComponentDrainTimeout(0),
		WithComponentStopTimeout(0),
		WithProbeTimeout(0),
	)
	if err != nil {
		t.Fatalf("New returned %v", err)
	}
	if service.config.startupTimeout != 0 || service.config.shutdownTimeout != 0 ||
		service.config.componentDrainTimeout != 0 || service.config.componentStopTimeout != 0 || service.config.probeTimeout != 0 {
		t.Fatalf("zero-duration options were not applied: %+v", service.config)
	}
}

func TestWithClock(t *testing.T) {
	t.Parallel()

	expected := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	service, err := New("clocked", WithClock(clock.NewFake(expected)))
	if err != nil {
		t.Fatalf("New returned %v", err)
	}
	if got := service.Snapshot().Since; !got.Equal(expected) {
		t.Fatalf("snapshot time = %v, want %v", got, expected)
	}
	if _, err := New("invalid-clock", WithClock(nil)); !errors.Is(err, ErrNilClock) {
		t.Fatalf("New(WithClock(nil)) error = %v", err)
	}
}
