// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package retry

import (
	"math"
	"time"
)

const (
	defaultMaxAttempts    = 3
	defaultInitialDelay   = 100 * time.Millisecond
	defaultMaximumDelay   = 5 * time.Second
	defaultMultiplier     = 2.0
	defaultJitterFraction = 0.20
)

// Policy is an immutable retry budget and delay strategy.
type Policy struct {
	maxAttempts    int
	maxElapsed     time.Duration
	backoff        Backoff
	jitterFraction float64
}

// PolicyOption configures a Policy.
type PolicyOption func(*policyConfiguration) error

type policyConfiguration struct {
	maxAttempts    int
	maxElapsed     time.Duration
	backoff        Backoff
	jitterFraction float64
}

// DefaultPolicy returns the conservative package default: three total
// attempts, exponential backoff from 100ms to 5s, and 20% symmetric jitter.
func DefaultPolicy() Policy {
	backoff, _ := ExponentialBackoff(defaultInitialDelay, defaultMaximumDelay, defaultMultiplier)
	return Policy{
		maxAttempts:    defaultMaxAttempts,
		backoff:        backoff,
		jitterFraction: defaultJitterFraction,
	}
}

// NewPolicy constructs and validates an immutable policy.
func NewPolicy(options ...PolicyOption) (Policy, error) {
	// Policy and policyConfiguration are the same shape by construction — the second exists so
	// PolicyOption has something mutable to write into. A conversion keeps them that way: add a
	// field to one without the other and this stops compiling, where a field-by-field literal
	// would silently leave the new field at its zero value.
	configuration := policyConfiguration(DefaultPolicy())
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&configuration); err != nil {
			return Policy{}, err
		}
	}
	policy := Policy(configuration)
	if err := policy.Validate(); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

// WithMaxAttempts sets the maximum total attempts, including the first.
func WithMaxAttempts(maximum int) PolicyOption {
	return func(configuration *policyConfiguration) error {
		configuration.maxAttempts = maximum
		return nil
	}
}

// WithMaxElapsed sets the total elapsed-time budget. Zero disables the
// additional elapsed-time limit; the context may still impose a deadline.
func WithMaxElapsed(maximum time.Duration) PolicyOption {
	return func(configuration *policyConfiguration) error {
		configuration.maxElapsed = maximum
		return nil
	}
}

// WithBackoff sets the delay strategy.
func WithBackoff(backoff Backoff) PolicyOption {
	return func(configuration *policyConfiguration) error {
		if nilBackoff(backoff) {
			return invalidPolicyField("backoff", "nil", ErrInvalidBackoff)
		}
		configuration.backoff = backoff
		return nil
	}
}

// WithJitterFraction sets symmetric jitter in [0,1]. For example, 0.2 samples
// delays from 80% through 120% of the base delay.
func WithJitterFraction(fraction float64) PolicyOption {
	return func(configuration *policyConfiguration) error {
		configuration.jitterFraction = fraction
		return nil
	}
}

// WithoutJitter disables delay jitter.
func WithoutJitter() PolicyOption { return WithJitterFraction(0) }

// Validate reports whether the policy is usable.
func (policy Policy) Validate() error {
	switch {
	case policy.maxAttempts <= 0:
		return invalidPolicyField("max_attempts", policy.maxAttempts, ErrInvalidPolicy)
	case policy.maxElapsed < 0:
		return invalidPolicyField("max_elapsed", policy.maxElapsed, ErrInvalidPolicy)
	case nilBackoff(policy.backoff):
		return invalidPolicyField("backoff", "nil", ErrInvalidBackoff)
	case math.IsNaN(policy.jitterFraction) || math.IsInf(policy.jitterFraction, 0) || policy.jitterFraction < 0 || policy.jitterFraction > 1:
		return invalidPolicyField("jitter_fraction", policy.jitterFraction, ErrInvalidJitter)
	default:
		return nil
	}
}

func (policy Policy) MaxAttempts() int          { return policy.maxAttempts }
func (policy Policy) MaxElapsed() time.Duration { return policy.maxElapsed }
func (policy Policy) Backoff() Backoff          { return policy.backoff }
func (policy Policy) JitterFraction() float64   { return policy.jitterFraction }
