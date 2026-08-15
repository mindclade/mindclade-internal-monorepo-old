// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package requestmeta

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"mindclade.internal/libs/go/faults"
)

func TestRequestIDConstructionAndSerializationEdges(t *testing.T) {
	t.Parallel()

	timestamp := time.Date(2026, 8, 12, 12, 34, 56, 0, time.UTC)
	requestID, err := NewRequestIDAt(timestamp)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := requestID.Time(); !ok || got.Before(timestamp.Truncate(time.Millisecond)) {
		t.Fatalf("Time() = %v, %v; want a process-monotonic timestamp at or after %v", got, ok, timestamp)
	}
	text, err := requestID.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var decoded RequestID
	if err := decoded.UnmarshalText(text); err != nil {
		t.Fatal(err)
	}
	if decoded != requestID {
		t.Fatalf("text round trip = %s", decoded)
	}
	if err := decoded.Scan([]byte(requestID.String())); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Scan(nil); err != nil || !decoded.IsZero() {
		t.Fatalf("Scan(nil) = %v, %v", decoded, err)
	}
	if err := decoded.Scan(42); !errors.Is(err, ErrInvalidRequestID) {
		t.Fatalf("Scan(int) error = %v", err)
	}

	var nilRequestID *RequestID
	if err := nilRequestID.UnmarshalJSON([]byte(`null`)); !errors.Is(err, ErrInvalidRequestID) {
		t.Fatalf("nil receiver error = %v", err)
	}
}

func TestTokenAndOperationSerialization(t *testing.T) {
	t.Parallel()

	requestID := MustParseRequestID(testRequestIDText)
	correlation, err := CorrelationIDFromRequestID(requestID)
	if err != nil {
		t.Fatal(err)
	}
	causation, err := CausationIDFromRequestID(requestID)
	if err != nil {
		t.Fatal(err)
	}
	operation := MustParseOperation("runs.Repository.Create")

	for name, value := range map[string]any{
		"correlation": correlation,
		"causation":   causation,
		"operation":   operation,
	} {
		name, value := name, value
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) == "null" {
				t.Fatalf("encoded zero value: %s", encoded)
			}
		})
	}

	var correlationRoundTrip CorrelationID
	text, err := correlation.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if err := correlationRoundTrip.UnmarshalText(text); err != nil || correlationRoundTrip != correlation {
		t.Fatalf("correlation round trip = %#v, %v", correlationRoundTrip, err)
	}
	if err := json.Unmarshal([]byte(`null`), &correlationRoundTrip); err != nil || !correlationRoundTrip.IsZero() {
		t.Fatalf("correlation null = %#v, %v", correlationRoundTrip, err)
	}

	var causationRoundTrip CausationID
	text, err = causation.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if err := causationRoundTrip.UnmarshalText(text); err != nil || causationRoundTrip != causation {
		t.Fatalf("causation round trip = %#v, %v", causationRoundTrip, err)
	}
	if err := json.Unmarshal([]byte(`null`), &causationRoundTrip); err != nil || !causationRoundTrip.IsZero() {
		t.Fatalf("causation null = %#v, %v", causationRoundTrip, err)
	}

	var operationRoundTrip Operation
	text, err = operation.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if err := operationRoundTrip.UnmarshalText(text); err != nil || operationRoundTrip != operation {
		t.Fatalf("operation round trip = %#v, %v", operationRoundTrip, err)
	}
	if err := json.Unmarshal([]byte(`null`), &operationRoundTrip); err != nil || !operationRoundTrip.IsZero() {
		t.Fatalf("operation null = %#v, %v", operationRoundTrip, err)
	}

	for _, invalid := range []string{".runs.create", "runs..create", "runs.create-", "1runs.create"} {
		if _, err := ParseOperation(invalid); !errors.Is(err, ErrInvalidOperation) {
			t.Fatalf("ParseOperation(%q) error = %v", invalid, err)
		}
	}

	var nilCorrelation *CorrelationID
	if err := nilCorrelation.UnmarshalJSON([]byte(`null`)); !errors.Is(err, ErrInvalidCorrelation) {
		t.Fatalf("nil correlation receiver error = %v", err)
	}
	var nilCausation *CausationID
	if err := nilCausation.UnmarshalJSON([]byte(`null`)); !errors.Is(err, ErrInvalidCausation) {
		t.Fatalf("nil causation receiver error = %v", err)
	}
	var nilOperation *Operation
	if err := nilOperation.UnmarshalJSON([]byte(`null`)); !errors.Is(err, ErrInvalidOperation) {
		t.Fatalf("nil operation receiver error = %v", err)
	}
}

func TestContextConvenience(t *testing.T) {
	t.Parallel()

	requestID := MustParseRequestID(testRequestIDText)
	correlation, _ := ParseCorrelationID("correlation-123")
	causation, _ := ParseCausationID("causation-123")
	operation := MustParseOperation("runs.Create")

	ctx := context.Background()
	var err error
	ctx, err = WithRequestID(ctx, requestID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = WithCorrelationID(ctx, correlation)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = WithCausationID(ctx, causation)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = WithOperation(ctx, operation)
	if err != nil {
		t.Fatal(err)
	}

	if got, ok := RequestIDFromContext(ctx); !ok || got != requestID {
		t.Fatalf("request ID = %#v, %v", got, ok)
	}
	if got, ok := CorrelationIDFromContext(ctx); !ok || got != correlation {
		t.Fatalf("correlation ID = %#v, %v", got, ok)
	}
	if got, ok := CausationIDFromContext(ctx); !ok || got != causation {
		t.Fatalf("causation ID = %#v, %v", got, ok)
	}
	if got, ok := OperationFromContext(ctx); !ok || got != operation {
		t.Fatalf("operation = %#v, %v", got, ok)
	}

}

func TestExtractOrGenerateAndTypedNilCarrier(t *testing.T) {
	t.Parallel()

	ctx, requestID, err := ExtractOrGenerate(context.Background(), MapCarrier{})
	if err != nil || requestID.IsZero() {
		t.Fatalf("ExtractOrGenerate() = %s, %v", requestID, err)
	}
	metadata, ok := FromContext(ctx)
	if !ok || metadata.RequestID != requestID || metadata.CorrelationID.String() != requestID.String() {
		t.Fatalf("metadata = %#v, %v", metadata, ok)
	}

	var nilMap MapCarrier
	if err := Inject(ctx, nilMap); !errors.Is(err, ErrNilCarrier) || !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("typed nil carrier error = %v", err)
	}
	if _, err := Extract(context.Background(), nilMap); !errors.Is(err, ErrNilCarrier) {
		t.Fatalf("typed nil extract error = %v", err)
	}
}
