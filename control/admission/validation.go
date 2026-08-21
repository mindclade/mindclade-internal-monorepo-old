// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admission

import (
	"reflect"
	"regexp"
	"strings"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)

func validateName(value, label string) error {
	if !namePattern.MatchString(value) {
		return invalid(label+"_invalid", label+" is invalid", nil)
	}
	return nil
}

func validateID(value identifiers.ID, kind, label string) error {
	if err := value.Validate(); err != nil || value.Kind().String() != kind {
		return invalid(label+"_invalid", label+" is invalid", err)
	}
	return nil
}

func invalid(reason, message string, cause error) error {
	options := []faults.Option{
		faults.WithReason(reason),
		faults.WithOperation("control.admission"),
		faults.WithRetryPolicy(faults.NoRetry()),
	}
	if cause != nil {
		return faults.Wrap(cause, faults.CodeInvalidArgument, message, options...)
	}
	return faults.New(faults.CodeInvalidArgument, message, options...)
}

func denied(reason, message string) error {
	return faults.New(faults.CodePermissionDenied, message,
		faults.WithReason(reason), faults.WithOperation("control.admission"),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func exhausted(reason, message string) error {
	return faults.New(faults.CodeResourceExhausted, message,
		faults.WithReason(reason), faults.WithOperation("control.admission"),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func conflict(reason, message string) error {
	return faults.New(faults.CodeConflict, message,
		faults.WithReason(reason), faults.WithOperation("control.admission"),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func failedPrecondition(reason, message string) error {
	return faults.New(faults.CodeFailedPrecondition, message,
		faults.WithReason(reason), faults.WithOperation("control.admission"),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func notFound(reason, message string) error {
	return faults.New(faults.CodeNotFound, message,
		faults.WithReason(reason), faults.WithOperation("control.admission"),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func unavailable(reason, message string, cause error) error {
	if cause == nil {
		return faults.New(faults.CodeUnavailable, message,
			faults.WithReason(reason), faults.WithOperation("control.admission"),
			faults.WithRetryPolicy(faults.BackoffRetry(3)))
	}
	return faults.Wrap(cause, faults.CodeUnavailable, message,
		faults.WithReason(reason), faults.WithOperation("control.admission"),
		faults.WithRetryPolicy(faults.BackoffRetry(3)))
}

func canonicalJoin(values ...string) string { return strings.Join(values, "\x1f") }

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
