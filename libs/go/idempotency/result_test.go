// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package idempotency

import (
	"encoding/json"
	"errors"
	"testing"

	"mindclade.internal/libs/go/faults"
)

func TestResultDefensiveCopyAndJSON(t *testing.T) {
	payload := []byte("ok")
	metadata := map[string]string{"model": "clade-1"}
	result, err := NewResult(payload, "application/json", metadata)
	if err != nil {
		t.Fatal(err)
	}
	payload[0] = 'X'
	metadata["model"] = "mutated"
	if string(result.Payload()) != "ok" || result.Metadata()["model"] != "clade-1" {
		t.Fatal("result mutated")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Result
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Digest().Equal(result.Digest()) {
		t.Fatal("digest mismatch")
	}
}
func TestResultRejectsSensitiveMetadata(t *testing.T) {
	_, err := NewResult(nil, "", map[string]string{"access_token": "secret"})
	if !faults.IsReason(err, ReasonInvalidResult) {
		t.Fatalf("error=%v", err)
	}
}

func TestResultJSONRequiresMatchingDigest(t *testing.T) {
	t.Parallel()

	var result Result
	if err := json.Unmarshal([]byte(`{"payload":"b2s="}`), &result); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("missing digest error = %v", err)
	}
	if err := json.Unmarshal([]byte(`{"payload":"b2s=","digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"}`), &result); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("mismatched digest error = %v", err)
	}
	var nilResult *Result
	if err := nilResult.UnmarshalJSON([]byte(`null`)); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("nil result receiver error = %v", err)
	}
}

func TestZeroResultJSONRepresentsAbsence(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(Result{})
	if err != nil || string(encoded) != "null" {
		t.Fatalf("zero result JSON = %s, %v", encoded, err)
	}
	result, err := NewResult([]byte("value"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte("null"), &result); err != nil || !result.IsZero() {
		t.Fatalf("null result = %#v, %v", result, err)
	}
}
