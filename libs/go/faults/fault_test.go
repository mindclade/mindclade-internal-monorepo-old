// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package faults

import (
	stderrors "errors"
	"testing"
)

func TestNewFault(t *testing.T) {
	t.Parallel()

	cause := stderrors.New("backend disconnected")
	err := New(
		CodeUnavailable,
		"worker unavailable",
		WithCause(cause),
		WithReason("worker_disconnected"),
		WithOperation("runs.Dispatch"),
		WithFields(Fields{
			FieldRunID: "run_123",
			"api_key":  "secret",
		}),
		WithRetryPolicy(BackoffRetry(5)),
	)

	fault, ok := AsFault(err)
	if !ok {
		t.Fatalf("AsFault() failed for %T", err)
	}

	if got := fault.Code(); got != CodeUnavailable {
		t.Fatalf("Code() = %q", got)
	}
	if got := fault.Message(); got != "worker unavailable" {
		t.Fatalf("Message() = %q", got)
	}
	if got := fault.Reason(); got != "worker_disconnected" {
		t.Fatalf("Reason() = %q", got)
	}
	if got := fault.Operation(); got != "runs.Dispatch" {
		t.Fatalf("Operation() = %q", got)
	}
	if got := fault.RetryPolicy(); got != BackoffRetry(5) {
		t.Fatalf("RetryPolicy() = %+v", got)
	}
	if !stderrors.Is(err, cause) {
		t.Fatal("errors.Is() did not find wrapped cause")
	}

	wantError := "runs.Dispatch: worker unavailable: backend disconnected"
	if got := err.Error(); got != wantError {
		t.Fatalf("Error() = %q, want %q", got, wantError)
	}

	fields := fault.Fields()
	if got := fields[FieldRunID]; got != "run_123" {
		t.Fatalf("run_id = %v", got)
	}
	if got := fields["api_key"]; got != RedactedValue {
		t.Fatalf("api_key = %v, want redacted", got)
	}
}

func TestFaultFieldsAreImmutableThroughAccessors(t *testing.T) {
	t.Parallel()

	input := Fields{"labels": []string{"a", "b"}}
	err := New(CodeInternal, "failed", WithFields(input))

	input["labels"].([]string)[0] = "mutated-input"

	fault, ok := AsFault(err)
	if !ok {
		t.Fatal("AsFault() = false")
	}

	first := fault.Fields()
	if got := first["labels"].([]string)[0]; got != "a" {
		t.Fatalf("stored field changed through input: %q", got)
	}

	first["labels"].([]string)[0] = "mutated-output"
	second := fault.Fields()
	if got := second["labels"].([]string)[0]; got != "a" {
		t.Fatalf("stored field changed through accessor: %q", got)
	}
}

func TestNewNormalizesInvalidCodeAndBlankMessage(t *testing.T) {
	t.Parallel()

	err := New(Code("made_up"), "   ")
	fault, ok := AsFault(err)
	if !ok {
		t.Fatal("AsFault() = false")
	}
	if got := fault.Code(); got != CodeUnknown {
		t.Fatalf("Code() = %q", got)
	}
	if got := fault.Message(); got != "operation failed" {
		t.Fatalf("Message() = %q", got)
	}
}

func TestWrapNilReturnsNil(t *testing.T) {
	t.Parallel()

	if got := Wrap(nil, CodeInternal, "failed"); got != nil {
		t.Fatalf("Wrap(nil, ...) = %v, want nil", got)
	}
}

func TestWrapCauseCanBeReplacedExplicitly(t *testing.T) {
	t.Parallel()

	first := stderrors.New("first")
	second := stderrors.New("second")
	err := Wrap(first, CodeInternal, "failed", WithCause(second))

	if stderrors.Is(err, first) {
		t.Fatal("errors.Is() unexpectedly found replaced cause")
	}
	if !stderrors.Is(err, second) {
		t.Fatal("errors.Is() did not find replacement cause")
	}
}

func TestNilFaultMethods(t *testing.T) {
	t.Parallel()

	var fault *Fault
	if got := fault.Error(); got != "<nil>" {
		t.Fatalf("Error() = %q", got)
	}
	if got := fault.Code(); got != CodeUnknown {
		t.Fatalf("Code() = %q", got)
	}
	if fault.Unwrap() != nil {
		t.Fatal("Unwrap() != nil")
	}
	if fault.Fields() != nil {
		t.Fatal("Fields() != nil")
	}
}
