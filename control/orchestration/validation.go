// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"reflect"
	"regexp"
	"strings"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

// operation is the faults.WithOperation value for every fault this package
// raises. Keeping it one constant is what makes control.orchestration a
// searchable dimension in telemetry rather than a set of near-miss spellings.
const operation = "control.orchestration"

// Bounds mirror the tightest consumer of a workload, not the loosest. Rust caps
// an operation at 256 bytes (worker_protocol/src/command.rs) and Python caps it
// at 128 (libs/python/worker_runtime/contracts.py), so a 200-byte operation Go
// accepted here would be rejected the moment it reached a Python engine. The
// same reasoning fixes every bound below: Go is the producer, so Go holds the
// intersection.
const (
	MaximumOperationLength       = 128
	MaximumOutputNamespaceLength = 128
	MaximumResourceClassLength   = 128
	MaximumArtifactCount         = 4096
	MaximumDependencyCount       = 4096
	MaximumReasonLength          = 1024
)

// operationPattern is the Rust charset from worker_protocol/src/command.rs
// valid_operation. Python additionally rejects control characters, which this
// pattern excludes by construction.
var operationPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-/:]*$`)

func invalid(reason, message string, cause error) error {
	options := []faults.Option{
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()),
	}
	if cause == nil {
		return faults.New(faults.CodeInvalidArgument, message, options...)
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, message, options...)
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

// unavailable is the only helper that admits a retry. Everything else in this
// package is a decision about durable state, where replaying an identical
// request cannot change the answer.
func unavailable(reason, message string, cause error) error {
	options := []faults.Option{
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.BackoffRetry(3)),
	}
	if cause == nil {
		return faults.New(faults.CodeUnavailable, message, options...)
	}
	return faults.Wrap(cause, faults.CodeUnavailable, message, options...)
}

// validateID rejects an identifier whose kind is wrong, not merely one that
// fails to parse. Rust checks the kind of every field in a workload envelope
// (worker_protocol/src/workload.rs), so a run ID accepted here in a stage field
// travels one hop before it is refused.
func validateID(value, kind, label string) error {
	parsed, err := identifiers.ParseID(value)
	if err != nil {
		return invalid(label+"_invalid", label+" must be a canonical identifier", err)
	}
	if parsed.Kind().String() != kind {
		return invalid(label+"_kind_invalid", label+" must name a "+kind, nil)
	}
	return nil
}

func validateOperation(value string) error {
	if value == "" || len(value) > MaximumOperationLength ||
		value != strings.TrimSpace(value) || !operationPattern.MatchString(value) {
		return invalid("operation_invalid", "operation is empty, oversized, or contains unsupported characters", nil)
	}
	return nil
}

func validateBoundedName(value, label string, maximum int) error {
	if value == "" || len(value) > maximum || value != strings.TrimSpace(value) {
		return invalid(label+"_invalid", label+" is empty, oversized, or not trimmed", nil)
	}
	return nil
}

func validateReason(value string) error {
	if value == "" || len(value) > MaximumReasonLength || value != strings.TrimSpace(value) {
		return invalid("reason_invalid", "reason is empty, oversized, or not trimmed", nil)
	}
	return nil
}

// canonicalJoin builds the digest preimage for a sealed record. The unit
// separator cannot appear in any validated field above, so no pair of distinct
// field lists can produce the same preimage.
func canonicalJoin(values ...string) string { return strings.Join(values, "\x1f") }

// nilInterface reports whether an interface holds a nil pointer. A typed nil is
// not == nil, so a service handed a (*Store)(nil) would pass a plain guard and
// panic on first use.
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
