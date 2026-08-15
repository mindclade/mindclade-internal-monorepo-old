// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package faults

import "time"

// RetryKind describes how a caller may retry an operation. It does not execute
// retries or prescribe a particular backoff algorithm.
type RetryKind string

const (
	RetryKindUnspecified RetryKind = ""
	RetryKindNever       RetryKind = "never"
	RetryKindImmediate   RetryKind = "immediate"
	RetryKindBackoff     RetryKind = "with_backoff"
	RetryKindAfter       RetryKind = "after"
)

// RetryPolicy communicates retry intent to a caller.
//
// MaxAttempts is the maximum total number of attempts, including the initial
// attempt. Zero delegates the limit to the caller's policy.
type RetryPolicy struct {
	Kind        RetryKind     `json:"kind,omitempty"`
	After       time.Duration `json:"after,omitempty"`
	MaxAttempts int           `json:"max_attempts,omitempty"`
}

// Valid reports whether the policy is already in canonical form.
func (policy RetryPolicy) Valid() bool {
	normalized := policy.Normalized()
	return policy == normalized
}

// Normalized returns a canonical and safe representation of the policy.
func (policy RetryPolicy) Normalized() RetryPolicy {
	if policy.MaxAttempts < 0 {
		policy.MaxAttempts = 0
	}

	switch policy.Kind {
	case RetryKindUnspecified:
		policy.After = 0
		policy.MaxAttempts = 0
	case RetryKindNever:
		policy.After = 0
		policy.MaxAttempts = 0
	case RetryKindImmediate:
		policy.After = 0
	case RetryKindBackoff:
		policy.After = 0
	case RetryKindAfter:
		if policy.After <= 0 {
			policy.Kind = RetryKindImmediate
			policy.After = 0
		}
	default:
		policy = RetryPolicy{}
	}

	return policy
}

// Specified reports whether the policy explicitly communicates retry intent.
func (policy RetryPolicy) Specified() bool {
	return policy.Normalized().Kind != RetryKindUnspecified
}

// Retryable reports whether the normalized policy permits another attempt.
func (policy RetryPolicy) Retryable() bool {
	normalized := policy.Normalized()
	if normalized.MaxAttempts == 1 {
		return false
	}

	switch normalized.Kind {
	case RetryKindImmediate, RetryKindBackoff, RetryKindAfter:
		return true
	default:
		return false
	}
}

// NoRetry returns an explicit policy that forbids retrying.
func NoRetry() RetryPolicy {
	return RetryPolicy{Kind: RetryKindNever}
}

// ImmediateRetry permits an immediate retry.
func ImmediateRetry(maxAttempts int) RetryPolicy {
	return RetryPolicy{
		Kind:        RetryKindImmediate,
		MaxAttempts: maxAttempts,
	}.Normalized()
}

// BackoffRetry permits retrying with a caller-selected backoff algorithm.
func BackoffRetry(maxAttempts int) RetryPolicy {
	return RetryPolicy{
		Kind:        RetryKindBackoff,
		MaxAttempts: maxAttempts,
	}.Normalized()
}

// DelayedRetry permits retrying after at least after has elapsed. A non-positive
// delay is normalized to an immediate retry.
func DelayedRetry(after time.Duration, maxAttempts int) RetryPolicy {
	return RetryPolicy{
		Kind:        RetryKindAfter,
		After:       after,
		MaxAttempts: maxAttempts,
	}.Normalized()
}
