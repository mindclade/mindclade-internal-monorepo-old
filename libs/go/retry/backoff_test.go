// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package retry

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestBackoffStrategies(t *testing.T) {
	t.Parallel()

	if delay := ImmediateBackoff().Delay(10); delay != 0 {
		t.Fatalf("ImmediateBackoff delay = %v, want 0", delay)
	}

	fixed, err := FixedBackoff(250 * time.Millisecond)
	if err != nil {
		t.Fatalf("FixedBackoff: %v", err)
	}
	if got := fixed.Delay(4); got != 250*time.Millisecond {
		t.Fatalf("fixed delay = %v", got)
	}

	exponential, err := ExponentialBackoff(100*time.Millisecond, 350*time.Millisecond, 2)
	if err != nil {
		t.Fatalf("ExponentialBackoff: %v", err)
	}
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 350 * time.Millisecond, 350 * time.Millisecond}
	for index, expected := range want {
		if got := exponential.Delay(index + 1); got != expected {
			t.Fatalf("retry %d delay = %v, want %v", index+1, got, expected)
		}
	}
}

func TestBackoffValidationAndOverflow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		call func() error
	}{
		{"negative fixed", func() error { _, err := FixedBackoff(-time.Second); return err }},
		{"negative initial", func() error { _, err := ExponentialBackoff(-1, time.Second, 2); return err }},
		{"small maximum", func() error { _, err := ExponentialBackoff(time.Second, time.Millisecond, 2); return err }},
		{"small multiplier", func() error { _, err := ExponentialBackoff(time.Second, 0, 0.5); return err }},
		{"nan multiplier", func() error { _, err := ExponentialBackoff(time.Second, 0, math.NaN()); return err }},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.call(); !errors.Is(err, ErrInvalidBackoff) {
				t.Fatalf("error = %v, want ErrInvalidBackoff", err)
			}
		})
	}

	exponential, err := ExponentialBackoff(time.Duration(math.MaxInt64/2), 0, 8)
	if err != nil {
		t.Fatalf("ExponentialBackoff: %v", err)
	}
	if got := exponential.Delay(4); got != time.Duration(math.MaxInt64) {
		t.Fatalf("overflow delay = %v", got)
	}
}

func TestApplyJitter(t *testing.T) {
	t.Parallel()

	base := time.Second
	if got := applyJitter(base, 0.2, 0); got != 800*time.Millisecond {
		t.Fatalf("lower jitter = %v", got)
	}
	if got := applyJitter(base, 0.2, 0.5); got != time.Second {
		t.Fatalf("center jitter = %v", got)
	}
	if got := applyJitter(base, 0.2, math.Nextafter(1, 0)); got < 1199*time.Millisecond || got > 1200*time.Millisecond {
		t.Fatalf("upper jitter = %v", got)
	}
	if got := applyJitter(0, 1, 0.5); got != 0 {
		t.Fatalf("zero jitter = %v", got)
	}
}
