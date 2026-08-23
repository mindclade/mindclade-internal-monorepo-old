// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import (
	"reflect"
	"regexp"
	"strings"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

// operation names every fault this package raises. Fleet scheduling and gateway
// admission both produce resource-exhausted faults for entirely different
// reasons, and an operator paging on "quota exhausted" needs to know which
// budget ran out before deciding whether to add nodes or raise a spend cap.
const operation = "control.scheduling"

// namePattern is the tenant/workspace/pool label alphabet. It is deliberately
// the same shape Kubernetes accepts for a label value, because every one of
// these names is eventually projected onto an object the API server validates;
// accepting a name here that the cluster later rejects turns a synchronous
// policy error into an asynchronous placement failure.
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
		faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()),
	}
	if cause != nil {
		return faults.Wrap(cause, faults.CodeInvalidArgument, message, options...)
	}
	return faults.New(faults.CodeInvalidArgument, message, options...)
}

func denied(reason, message string) error {
	return faults.New(faults.CodePermissionDenied, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func exhausted(reason, message string) error {
	return faults.New(faults.CodeResourceExhausted, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func conflict(reason, message string) error {
	return faults.New(faults.CodeConflict, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func failedPrecondition(reason, message string) error {
	return faults.New(faults.CodeFailedPrecondition, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func notFound(reason, message string) error {
	return faults.New(faults.CodeNotFound, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func unavailable(reason, message string, cause error) error {
	if cause == nil {
		return faults.New(faults.CodeUnavailable, message,
			faults.WithReason(reason), faults.WithOperation(operation),
			faults.WithRetryPolicy(faults.BackoffRetry(3)))
	}
	return faults.Wrap(cause, faults.CodeUnavailable, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.BackoffRetry(3)))
}

// canonicalJoin uses the ASCII unit separator so no component of a digest input
// can forge a boundary. A pool name or workload class containing a comma or a
// slash would otherwise let two different placements hash identically.
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
