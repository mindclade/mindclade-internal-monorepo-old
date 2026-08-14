// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package identifiers

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestPackageLevelGeneration(t *testing.T) {
	kind := MustParseKind("run")

	v4, err := NewUUIDv4()
	if err != nil {
		t.Fatal(err)
	}
	if v4.Version() != 4 || v4.Variant() != VariantRFC4122 {
		t.Fatalf("UUIDv4 version=%d variant=%d", v4.Version(), v4.Variant())
	}

	v7, err := NewUUIDv7()
	if err != nil {
		t.Fatal(err)
	}
	if v7.Version() != 7 || v7.Variant() != VariantRFC4122 {
		t.Fatalf("UUIDv7 version=%d variant=%d", v7.Version(), v7.Variant())
	}

	v7At, err := NewUUIDv7At(time.UnixMilli(1_000))
	if err != nil {
		t.Fatal(err)
	}
	if v7At.Version() != 7 {
		t.Fatalf("UUIDv7At version=%d", v7At.Version())
	}

	identifier, err := NewID(kind)
	if err != nil {
		t.Fatal(err)
	}
	if identifier.Kind() != kind || identifier.UUID().Version() != 7 {
		t.Fatalf("NewID() = %s", identifier)
	}

	identifierAt, err := NewIDAt(kind, time.UnixMilli(2_000))
	if err != nil {
		t.Fatal(err)
	}
	if identifierAt.Kind() != kind || !identifierAt.Valid() {
		t.Fatalf("NewIDAt() = %s", identifierAt)
	}
}

func TestIdentifierAdditionalSerializationPaths(t *testing.T) {
	t.Parallel()

	original := MustParseID("run_0000000003e870008000000000000000")
	text, err := original.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var fromText ID
	if err := fromText.UnmarshalText(text); err != nil {
		t.Fatal(err)
	}
	if fromText != original {
		t.Fatalf("text round trip = %s", fromText)
	}

	var scanned ID
	if err := scanned.Scan([]byte(original.String())); err != nil {
		t.Fatal(err)
	}
	if scanned != original {
		t.Fatalf("byte scan = %s", scanned)
	}
	if err := scanned.Scan(nil); err != nil || !scanned.IsZero() {
		t.Fatalf("nil scan = %s, %v", scanned, err)
	}
	if err := scanned.Scan(123); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("unsupported scan error = %v", err)
	}

	if err := json.Unmarshal([]byte("null"), &scanned); err != nil || !scanned.IsZero() {
		t.Fatalf("null JSON = %s, %v", scanned, err)
	}
	if err := json.Unmarshal([]byte("123"), &scanned); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("invalid JSON error = %v", err)
	}
}

func TestDigestAdditionalSerializationPaths(t *testing.T) {
	t.Parallel()

	original := SHA256String("artifact")
	parsed := MustParseDigest(original.String())
	if !parsed.Equal(original) {
		t.Fatal("MustParseDigest result differs")
	}

	text, err := original.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var fromText Digest
	if err := fromText.UnmarshalText(text); err != nil {
		t.Fatal(err)
	}
	if !fromText.Equal(original) {
		t.Fatalf("text round trip = %s", fromText)
	}
	if err := fromText.UnmarshalText(nil); err != nil || !fromText.IsZero() {
		t.Fatalf("empty text = %s, %v", fromText, err)
	}

	var scanned Digest
	if err := scanned.Scan([]byte(original.String())); err != nil {
		t.Fatal(err)
	}
	if !scanned.Equal(original) {
		t.Fatalf("text byte scan = %s", scanned)
	}
	if err := scanned.Scan(nil); err != nil || !scanned.IsZero() {
		t.Fatalf("nil scan = %s, %v", scanned, err)
	}
	if err := scanned.Scan(123); !errors.Is(err, ErrInvalidDigest) {
		t.Fatalf("unsupported scan error = %v", err)
	}

	value, err := (Digest{}).Value()
	if err != nil || value != nil {
		t.Fatalf("absent Value() = %v, %v", value, err)
	}
	if err := json.Unmarshal([]byte("null"), &scanned); err != nil || !scanned.IsZero() {
		t.Fatalf("null JSON = %s, %v", scanned, err)
	}
	if err := json.Unmarshal([]byte("123"), &scanned); !errors.Is(err, ErrInvalidDigest) {
		t.Fatalf("invalid JSON error = %v", err)
	}
}

func TestUUIDAdditionalSerializationAndVariants(t *testing.T) {
	t.Parallel()

	original := MustParseUUID("018f3f4a-5b6c-7d8e-8f90-0123456789ab")
	if got := original.URN(); got != "urn:uuid:"+original.String() {
		t.Fatalf("URN() = %q", got)
	}

	binary, err := original.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var fromBinary UUID
	if err := fromBinary.UnmarshalBinary(binary); err != nil {
		t.Fatal(err)
	}
	if fromBinary != original {
		t.Fatalf("binary round trip = %s", fromBinary)
	}

	value, err := original.Value()
	if err != nil || value != original.String() {
		t.Fatalf("Value() = %v, %v", value, err)
	}

	var scanned UUID
	if err := scanned.Scan([]byte(original.String())); err != nil {
		t.Fatal(err)
	}
	if scanned != original {
		t.Fatalf("text byte scan = %s", scanned)
	}
	if err := scanned.Scan(nil); err != nil || !scanned.IsZero() {
		t.Fatalf("nil scan = %s, %v", scanned, err)
	}
	if err := scanned.Scan(123); !errors.Is(err, ErrInvalidUUID) {
		t.Fatalf("unsupported scan error = %v", err)
	}

	if err := json.Unmarshal([]byte("null"), &scanned); err != nil || !scanned.IsZero() {
		t.Fatalf("null JSON = %s, %v", scanned, err)
	}
	if err := json.Unmarshal([]byte("123"), &scanned); !errors.Is(err, ErrInvalidUUID) {
		t.Fatalf("invalid JSON error = %v", err)
	}

	var ncs UUID
	ncs[8] = 0x00
	if ncs.Variant() != VariantNCS {
		t.Fatalf("NCS variant = %d", ncs.Variant())
	}
	var microsoft UUID
	microsoft[8] = 0xC0
	if microsoft.Variant() != VariantMicrosoft {
		t.Fatalf("Microsoft variant = %d", microsoft.Variant())
	}
	var future UUID
	future[8] = 0xE0
	if future.Variant() != VariantFuture {
		t.Fatalf("future variant = %d", future.Variant())
	}
}
