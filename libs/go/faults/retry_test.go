// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package faults

import (
	"testing"
	"time"
)

func TestRetryConstructors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		policy    RetryPolicy
		kind      RetryKind
		after     time.Duration
		attempts  int
		retryable bool
	}{
		{
			name:      "never",
			policy:    NoRetry(),
			kind:      RetryKindNever,
			retryable: false,
		},
		{
			name:      "immediate",
			policy:    ImmediateRetry(3),
			kind:      RetryKindImmediate,
			attempts:  3,
			retryable: true,
		},
		{
			name:      "backoff",
			policy:    BackoffRetry(5),
			kind:      RetryKindBackoff,
			attempts:  5,
			retryable: true,
		},
		{
			name:      "delayed",
			policy:    DelayedRetry(10*time.Second, 4),
			kind:      RetryKindAfter,
			after:     10 * time.Second,
			attempts:  4,
			retryable: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if !test.policy.Valid() {
				t.Fatalf("policy is invalid: %+v", test.policy)
			}
			if test.policy.Kind != test.kind {
				t.Fatalf("Kind = %q, want %q", test.policy.Kind, test.kind)
			}
			if test.policy.After != test.after {
				t.Fatalf("After = %v, want %v", test.policy.After, test.after)
			}
			if test.policy.MaxAttempts != test.attempts {
				t.Fatalf("MaxAttempts = %d, want %d", test.policy.MaxAttempts, test.attempts)
			}
			if got := test.policy.Retryable(); got != test.retryable {
				t.Fatalf("Retryable() = %v, want %v", got, test.retryable)
			}
		})
	}
}

func TestRetryPolicyNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   RetryPolicy
		want RetryPolicy
	}{
		{
			name: "unknown kind",
			in: RetryPolicy{
				Kind:        RetryKind("made_up"),
				After:       time.Second,
				MaxAttempts: 3,
			},
			want: RetryPolicy{},
		},
		{
			name: "negative attempts",
			in: RetryPolicy{
				Kind:        RetryKindBackoff,
				MaxAttempts: -10,
			},
			want: RetryPolicy{Kind: RetryKindBackoff},
		},
		{
			name: "never clears extra values",
			in: RetryPolicy{
				Kind:        RetryKindNever,
				After:       time.Second,
				MaxAttempts: 10,
			},
			want: RetryPolicy{Kind: RetryKindNever},
		},
		{
			name: "non-positive delay becomes immediate",
			in: RetryPolicy{
				Kind:        RetryKindAfter,
				After:       0,
				MaxAttempts: 3,
			},
			want: RetryPolicy{
				Kind:        RetryKindImmediate,
				MaxAttempts: 3,
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.in.Normalized(); got != test.want {
				t.Fatalf("Normalized() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestRetryableHonorsMaximumAttempts(t *testing.T) {
	t.Parallel()

	policy := ImmediateRetry(1)
	if policy.Retryable() {
		t.Fatal("Retryable() = true for a one-attempt policy")
	}
}

func TestUnspecifiedRetryPolicy(t *testing.T) {
	t.Parallel()

	var policy RetryPolicy
	if policy.Specified() {
		t.Fatal("zero policy should be unspecified")
	}
	if policy.Retryable() {
		t.Fatal("zero policy should not be retryable")
	}
	if !policy.Valid() {
		t.Fatal("zero policy should be valid")
	}
}
