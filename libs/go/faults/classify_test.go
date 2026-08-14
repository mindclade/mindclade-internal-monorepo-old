// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package faults

import (
	"context"
	stderrors "errors"
	"fmt"
	"io/fs"
	"testing"
)

type codedTestError struct {
	code Code
}

func (err codedTestError) Error() string { return "coded test error" }
func (err codedTestError) Code() Code    { return err.code }

func TestCodeOfStructuredFault(t *testing.T) {
	t.Parallel()

	err := New(CodeConflict, "release changed")
	if got := CodeOf(err); got != CodeConflict {
		t.Fatalf("CodeOf() = %q, want %q", got, CodeConflict)
	}
	if !IsCode(err, CodeConflict) {
		t.Fatal("IsCode() = false")
	}
}

func TestCodeOfHonorsOutermostStructuredClassification(t *testing.T) {
	t.Parallel()

	err := Wrap(context.DeadlineExceeded, CodeUnavailable, "storage unavailable")
	if got := CodeOf(err); got != CodeUnavailable {
		t.Fatalf("CodeOf() = %q, want %q", got, CodeUnavailable)
	}
}

func TestCodeOfCustomProvider(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("outer: %w", codedTestError{code: CodeAborted})
	if got := CodeOf(err); got != CodeAborted {
		t.Fatalf("CodeOf() = %q, want %q", got, CodeAborted)
	}
}

func TestCodeOfStandardLibraryErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want Code
	}{
		{name: "canceled", err: context.Canceled, want: CodeCanceled},
		{name: "deadline", err: context.DeadlineExceeded, want: CodeDeadlineExceeded},
		{name: "not found", err: fmt.Errorf("read: %w", fs.ErrNotExist), want: CodeNotFound},
		{name: "exists", err: fmt.Errorf("create: %w", fs.ErrExist), want: CodeAlreadyExists},
		{name: "permission", err: fmt.Errorf("open: %w", fs.ErrPermission), want: CodePermissionDenied},
		{name: "invalid", err: fmt.Errorf("open: %w", fs.ErrInvalid), want: CodeInvalidArgument},
		{name: "unknown", err: stderrors.New("boom"), want: CodeUnknown},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := CodeOf(test.err); got != test.want {
				t.Fatalf("CodeOf() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestClassificationAccessors(t *testing.T) {
	t.Parallel()

	err := New(
		CodeFailedPrecondition,
		"run is not qualified",
		WithReason("qualification_incomplete"),
		WithOperation("releases.Promote"),
		WithField(FieldRunID, "run_123"),
		WithRetryPolicy(NoRetry()),
	)

	if got := MessageOf(err); got != "run is not qualified" {
		t.Fatalf("MessageOf() = %q", got)
	}
	if got := PublicMessageOf(err); got != "run is not qualified" {
		t.Fatalf("PublicMessageOf() = %q", got)
	}
	if got := ReasonOf(err); got != "qualification_incomplete" {
		t.Fatalf("ReasonOf() = %q", got)
	}
	if got := OperationOf(err); got != "releases.Promote" {
		t.Fatalf("OperationOf() = %q", got)
	}
	if got := FieldsOf(err)[FieldRunID]; got != "run_123" {
		t.Fatalf("FieldsOf()[run_id] = %v", got)
	}
	if !IsReason(err, "qualification_incomplete") {
		t.Fatal("IsReason() = false")
	}
	if IsRetryable(err) {
		t.Fatal("IsRetryable() = true for no-retry policy")
	}
}

func TestRetryClassification(t *testing.T) {
	t.Parallel()

	err := New(
		CodeUnavailable,
		"worker unavailable",
		WithRetryPolicy(BackoffRetry(5)),
	)
	if !IsRetryable(err) {
		t.Fatal("IsRetryable() = false")
	}
	if got := RetryPolicyOf(err); got != BackoffRetry(5) {
		t.Fatalf("RetryPolicyOf() = %+v", got)
	}
}

func TestPublicMessageDoesNotExposeUnstructuredError(t *testing.T) {
	t.Parallel()

	err := stderrors.New("database password was rejected")
	if got := PublicMessageOf(err); got != "operation failed" {
		t.Fatalf("PublicMessageOf() = %q", got)
	}
	if got := MessageOf(err); got != err.Error() {
		t.Fatalf("MessageOf() = %q, want raw diagnostic", got)
	}
}

func TestInvalidClassificationMatchersDoNotMatch(t *testing.T) {
	t.Parallel()

	err := stderrors.New("boom")
	if IsCode(err, Code("made_up")) {
		t.Fatal("IsCode() matched an invalid target code")
	}
	if IsReason(err, "   ") {
		t.Fatal("IsReason() matched a blank target reason")
	}
}

func TestNilClassification(t *testing.T) {
	t.Parallel()

	if _, ok := AsFault(nil); ok {
		t.Fatal("AsFault(nil) found a fault")
	}
	if got := CodeOf(nil); got != CodeUnknown {
		t.Fatalf("CodeOf(nil) = %q", got)
	}
	if got := PublicMessageOf(nil); got != "" {
		t.Fatalf("PublicMessageOf(nil) = %q", got)
	}
	if IsCode(nil, CodeUnknown) {
		t.Fatal("IsCode(nil, unknown) = true")
	}
}
