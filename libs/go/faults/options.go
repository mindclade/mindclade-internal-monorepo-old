// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package faults

import (
	"context"
	"strings"
)

// Option configures a Fault during construction.
type Option func(*Fault)

// WithCause sets the wrapped diagnostic cause. Wrap supplies its cause before
// applying options, so a later WithCause option intentionally replaces it.
func WithCause(cause error) Option {
	return func(fault *Fault) {
		if fault == nil {
			return
		}
		fault.cause = cause
	}
}

// WithReason sets domain-specific machine-readable detail. Reasons should be
// stable lower_snake_case identifiers, such as "qualification_incomplete".
func WithReason(reason string) Option {
	return func(fault *Fault) {
		if fault == nil {
			return
		}
		fault.reason = strings.TrimSpace(reason)
	}
}

// WithOperation records the logical operation that failed.
func WithOperation(operation string) Option {
	return func(fault *Fault) {
		if fault == nil {
			return
		}
		fault.operation = strings.TrimSpace(operation)
	}
}

// WithField adds or replaces one structured diagnostic field.
func WithField(key string, value any) Option {
	return func(fault *Fault) {
		if fault == nil {
			return
		}
		fault.fields = mergeFields(fault.fields, Fields{key: value})
	}
}

// WithFields adds or replaces structured diagnostic fields.
func WithFields(fields Fields) Option {
	captured := cloneFields(fields)
	return func(fault *Fault) {
		if fault == nil {
			return
		}
		fault.fields = mergeFields(fault.fields, captured)
	}
}

// WithRetryPolicy records explicit retry intent.
func WithRetryPolicy(policy RetryPolicy) Option {
	normalized := policy.Normalized()
	return func(fault *Fault) {
		if fault == nil {
			return
		}
		fault.retry = normalized
	}
}

// WithRequestID adds a request identifier as structured metadata.
func WithRequestID(requestID string) Option {
	normalized := strings.TrimSpace(requestID)
	if normalized == "" {
		return func(*Fault) {}
	}
	return WithField(FieldRequestID, normalized)
}

// WithTraceID adds a trace identifier as structured metadata.
func WithTraceID(traceID string) Option {
	normalized := strings.TrimSpace(traceID)
	if normalized == "" {
		return func(*Fault) {}
	}
	return WithField(FieldTraceID, normalized)
}

// WithContextMetadata copies request, trace, and operation metadata from ctx as
// fallbacks. Values already present on the fault are not overwritten.
func WithContextMetadata(ctx context.Context) Option {
	return func(fault *Fault) {
		if fault == nil || ctx == nil {
			return
		}

		if fault.operation == "" {
			if operation, ok := OperationFromContext(ctx); ok {
				fault.operation = operation
			}
		}

		if requestID, ok := RequestIDFromContext(ctx); ok {
			if _, exists := fault.fields[FieldRequestID]; !exists {
				fault.fields = mergeFields(fault.fields, Fields{FieldRequestID: requestID})
			}
		}

		if traceID, ok := TraceIDFromContext(ctx); ok {
			if _, exists := fault.fields[FieldTraceID]; !exists {
				fault.fields = mergeFields(fault.fields, Fields{FieldTraceID: traceID})
			}
		}
	}
}
