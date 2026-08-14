// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package retry

import (
	"math"
	"reflect"
	"time"
)

// Backoff computes the delay before a retry. retryNumber is one for the first
// retry after the initial attempt.
type Backoff interface {
	Delay(retryNumber int) time.Duration
}

// BackoffFunc adapts a function to Backoff.
type BackoffFunc func(int) time.Duration

func (function BackoffFunc) Delay(retryNumber int) time.Duration {
	if function == nil {
		return 0
	}
	return function(retryNumber)
}

type immediateBackoff struct{}

func (immediateBackoff) Delay(int) time.Duration { return 0 }

// ImmediateBackoff returns a strategy with no delay.
func ImmediateBackoff() Backoff { return immediateBackoff{} }

type fixedBackoff struct{ delay time.Duration }

func (backoff fixedBackoff) Delay(int) time.Duration { return backoff.delay }

// FixedBackoff returns a constant-delay strategy.
func FixedBackoff(delay time.Duration) (Backoff, error) {
	if delay < 0 {
		return nil, invalidPolicyField("backoff.delay", delay, ErrInvalidBackoff)
	}
	return fixedBackoff{delay: delay}, nil
}

type exponentialBackoff struct {
	initial    time.Duration
	maximum    time.Duration
	multiplier float64
}

// ExponentialBackoff returns a saturating exponential strategy.
func ExponentialBackoff(initial, maximum time.Duration, multiplier float64) (Backoff, error) {
	switch {
	case initial < 0:
		return nil, invalidPolicyField("backoff.initial", initial, ErrInvalidBackoff)
	case maximum < 0:
		return nil, invalidPolicyField("backoff.maximum", maximum, ErrInvalidBackoff)
	case maximum > 0 && maximum < initial:
		return nil, invalidPolicyField("backoff.maximum", maximum, ErrInvalidBackoff)
	case math.IsNaN(multiplier) || math.IsInf(multiplier, 0) || multiplier < 1:
		return nil, invalidPolicyField("backoff.multiplier", multiplier, ErrInvalidBackoff)
	}
	return exponentialBackoff{initial: initial, maximum: maximum, multiplier: multiplier}, nil
}

func (backoff exponentialBackoff) Delay(retryNumber int) time.Duration {
	if retryNumber <= 0 || backoff.initial <= 0 {
		return 0
	}
	if retryNumber == 1 || backoff.multiplier == 1 {
		return clampDuration(backoff.initial, backoff.maximum)
	}

	value := float64(backoff.initial) * math.Pow(backoff.multiplier, float64(retryNumber-1))
	if math.IsInf(value, 1) || value >= float64(math.MaxInt64) {
		if backoff.maximum > 0 {
			return backoff.maximum
		}
		return time.Duration(math.MaxInt64)
	}
	return clampDuration(time.Duration(value), backoff.maximum)
}

func clampDuration(value, maximum time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	if maximum > 0 && value > maximum {
		return maximum
	}
	return value
}

func nilBackoff(backoff Backoff) bool {
	if backoff == nil {
		return true
	}
	value := reflect.ValueOf(backoff)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
