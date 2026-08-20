// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package requestmeta

import (
	"context"
	"errors"
	"testing"

	"go.mindclade.dev/libs/go/faults"
)

func TestContextMetadataAndFaultBridge(t *testing.T) {
	t.Parallel()

	requestID := MustParseRequestID(testRequestIDText)
	operation := MustParseOperation("runs.Create")
	ctx, err := WithMetadata(context.Background(), Metadata{
		RequestID: requestID,
		Operation: operation,
	})
	if err != nil {
		t.Fatal(err)
	}

	metadata, ok := FromContext(ctx)
	if !ok || metadata.RequestID != requestID || metadata.Operation != operation {
		t.Fatalf("FromContext() = %#v, %v", metadata, ok)
	}
	if got, ok := faults.RequestIDFromContext(ctx); !ok || got != requestID.String() {
		t.Fatalf("fault request ID = %q, %v", got, ok)
	}
	if got, ok := faults.OperationFromContext(ctx); !ok || got != operation.String() {
		t.Fatalf("fault operation = %q, %v", got, ok)
	}
}

func TestEnsureAndRequireRequestID(t *testing.T) {
	t.Parallel()

	ctx, requestID, err := EnsureRequestID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if requestID.IsZero() {
		t.Fatal("generated zero request ID")
	}
	resolved, err := RequireRequestID(ctx)
	if err != nil || resolved != requestID {
		t.Fatalf("RequireRequestID() = %s, %v", resolved.String(), err)
	}
	if _, err := RequireRequestID(context.Background()); !errors.Is(err, ErrInvalidRequestID) {
		t.Fatalf("missing request ID error = %v", err)
	}
}

func TestContextRejectsNil(t *testing.T) {
	t.Parallel()

	_, err := WithMetadata(nil, Metadata{})
	if !errors.Is(err, ErrNilContext) || !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("error = %v", err)
	}
	if _, ok := FromContext(nil); ok {
		t.Fatal("nil context returned metadata")
	}
}

func TestEnsureRequestIDPreservesInboundLineage(t *testing.T) {
	t.Parallel()

	correlation, err := ParseCorrelationID("external-flow-123")
	if err != nil {
		t.Fatal(err)
	}
	causation, err := ParseCausationID("message-456")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := WithMetadata(context.Background(), Metadata{CorrelationID: correlation, CausationID: causation})
	if err != nil {
		t.Fatal(err)
	}
	ctx, requestID, err := EnsureRequestID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	metadata, ok := FromContext(ctx)
	if !ok || metadata.RequestID != requestID || metadata.CorrelationID != correlation || metadata.CausationID != causation {
		t.Fatalf("metadata = %#v, %v", metadata, ok)
	}
}
