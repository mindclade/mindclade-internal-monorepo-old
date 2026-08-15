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

func TestDefaultPolicy(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	if err := policy.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if policy.MaxAttempts() != 3 {
		t.Fatalf("MaxAttempts = %d", policy.MaxAttempts())
	}
	if policy.MaxElapsed() != 0 {
		t.Fatalf("MaxElapsed = %v", policy.MaxElapsed())
	}
	if policy.JitterFraction() != 0.2 {
		t.Fatalf("JitterFraction = %v", policy.JitterFraction())
	}
}

func TestNewPolicyOptions(t *testing.T) {
	t.Parallel()

	backoff, err := FixedBackoff(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewPolicy(
		WithMaxAttempts(7),
		WithMaxElapsed(time.Minute),
		WithBackoff(backoff),
		WithoutJitter(),
		nil,
	)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	if policy.MaxAttempts() != 7 || policy.MaxElapsed() != time.Minute || policy.JitterFraction() != 0 {
		t.Fatalf("unexpected policy: %+v", policy)
	}
	if got := policy.Backoff().Delay(2); got != time.Second {
		t.Fatalf("delay = %v", got)
	}
}

func TestPolicyValidation(t *testing.T) {
	t.Parallel()

	var nilFunction BackoffFunc
	cases := []struct {
		name string
		opts []PolicyOption
		want error
	}{
		{"attempts", []PolicyOption{WithMaxAttempts(0)}, ErrInvalidPolicy},
		{"elapsed", []PolicyOption{WithMaxElapsed(-time.Second)}, ErrInvalidPolicy},
		{"jitter negative", []PolicyOption{WithJitterFraction(-0.1)}, ErrInvalidJitter},
		{"jitter large", []PolicyOption{WithJitterFraction(1.1)}, ErrInvalidJitter},
		{"jitter nan", []PolicyOption{WithJitterFraction(math.NaN())}, ErrInvalidJitter},
		{"nil backoff", []PolicyOption{WithBackoff(nil)}, ErrInvalidBackoff},
		{"typed nil backoff", []PolicyOption{WithBackoff(nilFunction)}, ErrInvalidBackoff},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewPolicy(test.opts...)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
