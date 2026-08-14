// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package requestmeta

import (
	"context"
	"errors"
	"testing"
)

func TestPropagationRoundTrip(t *testing.T) {
	t.Parallel()

	requestID := MustParseRequestID(testRequestIDText)
	correlationID, _ := ParseCorrelationID("corr-123")
	causationID, _ := ParseCausationID("cause-456")
	operation := MustParseOperation("local.Operation")

	ctx, err := WithMetadata(context.Background(), Metadata{
		RequestID: requestID, CorrelationID: correlationID,
		CausationID: causationID, Operation: operation,
	})
	if err != nil {
		t.Fatal(err)
	}
	carrier := MapCarrier{}
	if err := Inject(ctx, carrier); err != nil {
		t.Fatal(err)
	}
	if carrier.Get(PropagationKeyRequestID) != requestID.String() {
		t.Fatalf("carrier = %#v", carrier)
	}

	extracted, err := Extract(context.Background(), carrier)
	if err != nil {
		t.Fatal(err)
	}
	metadata, ok := FromContext(extracted)
	if !ok || metadata.RequestID != requestID || metadata.CorrelationID != correlationID || metadata.CausationID != causationID {
		t.Fatalf("metadata = %#v, %v", metadata, ok)
	}
	if !metadata.Operation.IsZero() {
		t.Fatalf("operation was propagated: %s", metadata.Operation.String())
	}
}

func TestPropagationFailsClosed(t *testing.T) {
	t.Parallel()

	carrier := MapCarrier{PropagationKeyRequestID: "not-a-request-id"}
	_, err := Extract(context.Background(), carrier)
	if !errors.Is(err, ErrInvalidRequestID) {
		t.Fatalf("error = %v", err)
	}
	if err := Inject(context.Background(), nil); !errors.Is(err, ErrNilCarrier) {
		t.Fatalf("nil carrier error = %v", err)
	}
}
