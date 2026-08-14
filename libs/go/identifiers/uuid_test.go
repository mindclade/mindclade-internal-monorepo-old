// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package identifiers

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestParseUUIDRepresentations(t *testing.T) {
	t.Parallel()

	canonical := "018f3f4a-5b6c-7d8e-8f90-0123456789ab"
	compact := "018f3f4a5b6c7d8e8f900123456789ab"

	for _, value := range []string{canonical, compact, "urn:uuid:" + canonical, "018F3F4A-5B6C-7D8E-8F90-0123456789AB"} {
		uuid, err := ParseUUID(value)
		if err != nil {
			t.Fatalf("ParseUUID(%q) error = %v", value, err)
		}
		if got := uuid.String(); got != canonical {
			t.Fatalf("String() = %q", got)
		}
		if got := uuid.Compact(); got != compact {
			t.Fatalf("Compact() = %q", got)
		}
	}
}

func TestParseUUIDRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "abc", "018f3f4a_5b6c-7d8e-8f90-0123456789ab", "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz"} {
		_, err := ParseUUID(value)
		if !errors.Is(err, ErrInvalidUUID) {
			t.Fatalf("ParseUUID(%q) error = %v", value, err)
		}
	}
}

func TestUUIDVersionVariantAndTime(t *testing.T) {
	t.Parallel()

	timestamp := time.Date(2026, time.August, 12, 17, 30, 0, 123_000_000, time.UTC)
	generator, err := NewGenerator(
		WithTimeSource(func() time.Time { return timestamp }),
		WithEntropySource(zeroReader{}),
	)
	if err != nil {
		t.Fatal(err)
	}

	uuid, err := generator.UUIDv7()
	if err != nil {
		t.Fatal(err)
	}
	if uuid.Version() != 7 || uuid.Variant() != VariantRFC4122 {
		t.Fatalf("version=%d variant=%d", uuid.Version(), uuid.Variant())
	}
	gotTime, ok := uuid.Time()
	if !ok || !gotTime.Equal(time.UnixMilli(timestamp.UnixMilli()).UTC()) {
		t.Fatalf("Time() = %s, %v", gotTime, ok)
	}

	v4, err := generator.UUIDv4()
	if err != nil {
		t.Fatal(err)
	}
	if v4.Version() != 4 || v4.Variant() != VariantRFC4122 {
		t.Fatalf("v4 version=%d variant=%d", v4.Version(), v4.Variant())
	}
	if _, ok := v4.Time(); ok {
		t.Fatal("UUIDv4 reported a UUIDv7 timestamp")
	}
}

func TestUUIDSerializationAndSQL(t *testing.T) {
	t.Parallel()

	original := MustParseUUID("018f3f4a-5b6c-7d8e-8f90-0123456789ab")

	text, err := original.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var fromText UUID
	if err := fromText.UnmarshalText(text); err != nil {
		t.Fatal(err)
	}
	if fromText != original {
		t.Fatalf("text round trip = %s", fromText)
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var fromJSON UUID
	if err := json.Unmarshal(encoded, &fromJSON); err != nil {
		t.Fatal(err)
	}
	if fromJSON != original {
		t.Fatalf("json round trip = %s", fromJSON)
	}

	var scanned UUID
	if err := scanned.Scan(original.Bytes()); err != nil {
		t.Fatal(err)
	}
	if scanned != original {
		t.Fatalf("binary scan = %s", scanned)
	}
	if err := scanned.Scan(original.String()); err != nil {
		t.Fatal(err)
	}
	if scanned != original {
		t.Fatalf("text scan = %s", scanned)
	}
}

func TestUUIDCompare(t *testing.T) {
	t.Parallel()

	left := MustParseUUID("00000000-0000-7000-8000-000000000001")
	right := MustParseUUID("00000000-0000-7000-8000-000000000002")
	if left.Compare(right) >= 0 || !left.Less(right) {
		t.Fatalf("unexpected ordering: %s, %s", left, right)
	}
}

type zeroReader struct{}

func (zeroReader) Read(value []byte) (int, error) {
	clear(value)
	return len(value), nil
}
