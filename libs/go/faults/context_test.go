// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package faults

import (
	"context"
	"testing"
)

func TestContextMetadataRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = ContextWithRequestID(ctx, "  req_123  ")
	ctx = ContextWithTraceID(ctx, "trace_123")
	ctx = ContextWithOperation(ctx, "runs.Create")

	if got, ok := RequestIDFromContext(ctx); !ok || got != "req_123" {
		t.Fatalf("RequestIDFromContext() = %q, %v", got, ok)
	}
	if got, ok := TraceIDFromContext(ctx); !ok || got != "trace_123" {
		t.Fatalf("TraceIDFromContext() = %q, %v", got, ok)
	}
	if got, ok := OperationFromContext(ctx); !ok || got != "runs.Create" {
		t.Fatalf("OperationFromContext() = %q, %v", got, ok)
	}
}

func TestContextBlankMetadataLeavesParentUnchanged(t *testing.T) {
	t.Parallel()

	parent := ContextWithRequestID(context.Background(), "req_123")
	child := ContextWithRequestID(parent, "   ")

	if child != parent {
		t.Fatal("blank metadata should return the parent context")
	}
	if got, ok := RequestIDFromContext(child); !ok || got != "req_123" {
		t.Fatalf("RequestIDFromContext() = %q, %v", got, ok)
	}
}

func TestContextRetrievalHandlesNil(t *testing.T) {
	t.Parallel()

	if got, ok := RequestIDFromContext(nil); ok || got != "" {
		t.Fatalf("RequestIDFromContext(nil) = %q, %v", got, ok)
	}
}

func TestContextInsertionRejectsNil(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("ContextWithRequestID(nil, ...) did not panic")
		}
	}()
	_ = ContextWithRequestID(nil, "req_123")
}

func TestWithContextMetadataUsesFallbackSemantics(t *testing.T) {
	t.Parallel()

	ctx := ContextWithRequestID(context.Background(), "req_from_context")
	ctx = ContextWithTraceID(ctx, "trace_from_context")
	ctx = ContextWithOperation(ctx, "context.Operation")

	err := New(
		CodeInternal,
		"failed",
		WithRequestID("req_explicit"),
		WithOperation("explicit.Operation"),
		WithContextMetadata(ctx),
	)

	fault, ok := AsFault(err)
	if !ok {
		t.Fatalf("AsFault() failed for %T", err)
	}
	if got := fault.Operation(); got != "explicit.Operation" {
		t.Fatalf("Operation() = %q", got)
	}

	fields := fault.Fields()
	if got := fields[FieldRequestID]; got != "req_explicit" {
		t.Fatalf("request_id = %v", got)
	}
	if got := fields[FieldTraceID]; got != "trace_from_context" {
		t.Fatalf("trace_id = %v", got)
	}
}
