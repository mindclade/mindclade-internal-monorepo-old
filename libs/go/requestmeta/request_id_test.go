// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package requestmeta

import (
	"encoding/json"
	"errors"
	"testing"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
)

const testRequestIDText = "request_018f3f4a5b6c7d8e8f900123456789ab"

func TestRequestIDRoundTrip(t *testing.T) {
	t.Parallel()

	requestID, err := ParseRequestID(testRequestIDText)
	if err != nil {
		t.Fatal(err)
	}
	if got := requestID.String(); got != testRequestIDText {
		t.Fatalf("String() = %q", got)
	}
	if requestID.ID().Kind() != RequestIDKind || requestID.IsZero() || !requestID.Valid() {
		t.Fatalf("unexpected request ID: %#v", requestID)
	}
	if _, ok := requestID.Time(); !ok {
		t.Fatal("request ID did not expose UUIDv7 timestamp")
	}

	encoded, err := json.Marshal(requestID)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RequestID
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != requestID {
		t.Fatalf("JSON round trip = %s", decoded.String())
	}

	var scanned RequestID
	if err := scanned.Scan(requestID.String()); err != nil {
		t.Fatal(err)
	}
	if scanned != requestID {
		t.Fatalf("SQL round trip = %s", scanned.String())
	}
}

func TestRequestIDRejectsWrongKind(t *testing.T) {
	t.Parallel()

	identifier := identifiers.MustParseID("run_018f3f4a5b6c7d8e8f900123456789ab")
	_, err := RequestIDFromID(identifier)
	if !errors.Is(err, ErrInvalidRequestID) {
		t.Fatalf("error = %v", err)
	}
	if !faults.IsCode(err, faults.CodeInvalidArgument) || !faults.IsReason(err, "invalid_request_id") {
		t.Fatalf("classification = %s/%s", faults.CodeOf(err), faults.ReasonOf(err))
	}
}

func TestRequestIDZeroSerialization(t *testing.T) {
	t.Parallel()

	var requestID RequestID
	if value, err := requestID.Value(); err != nil || value != nil {
		t.Fatalf("Value() = %#v, %v", value, err)
	}
	encoded, err := json.Marshal(requestID)
	if err != nil || string(encoded) != "null" {
		t.Fatalf("MarshalJSON() = %s, %v", encoded, err)
	}
	if requestID.Validate() == nil {
		t.Fatal("zero RequestID validated")
	}
}
