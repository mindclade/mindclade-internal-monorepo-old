// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package identifiers

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestIDRoundTrip(t *testing.T) {
	t.Parallel()

	kind := MustParseKind("run")
	generator, err := NewGenerator(WithEntropySource(zeroReader{}))
	if err != nil {
		t.Fatal(err)
	}
	identifier, err := generator.IDAt(kind, time.UnixMilli(1_000))
	if err != nil {
		t.Fatal(err)
	}

	if got := identifier.String(); got != "run_0000000003e870008000000000000000" {
		t.Fatalf("String() = %q", got)
	}
	parsed, err := ParseID(identifier.String())
	if err != nil {
		t.Fatal(err)
	}
	if parsed != identifier || parsed.Kind() != kind || !parsed.Valid() {
		t.Fatalf("parsed=%v kind=%q valid=%v", parsed, parsed.Kind(), parsed.Valid())
	}
	if gotTime, ok := parsed.Time(); !ok || gotTime.UnixMilli() != 1_000 {
		t.Fatalf("Time() = %s, %v", gotTime, ok)
	}
}

func TestParseIDRejectsNonCanonicalAndWrongVersion(t *testing.T) {
	t.Parallel()

	values := []string{
		"RUN_0000000003e870008000000000000000",
		"run_0000000003E870008000000000000000",
		"run_0000000003e840008000000000000000",
		"run__0000000003e870008000000000000000",
		"run_short",
	}
	for _, value := range values {
		_, err := ParseID(value)
		if !errors.Is(err, ErrInvalidID) {
			t.Fatalf("ParseID(%q) error = %v", value, err)
		}
	}
}

func TestParseIDKind(t *testing.T) {
	t.Parallel()

	value := "run_0000000003e870008000000000000000"
	if _, err := ParseIDKind(value, MustParseKind("run")); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseIDKind(value, MustParseKind("job")); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("ParseIDKind() error = %v", err)
	}
}

func TestIDSerializationAndSQL(t *testing.T) {
	t.Parallel()

	original := MustParseID("run_0000000003e870008000000000000000")

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ID
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != original {
		t.Fatalf("json round trip = %s", decoded)
	}

	value, err := original.Value()
	if err != nil {
		t.Fatal(err)
	}
	var scanned ID
	if err := scanned.Scan(value); err != nil {
		t.Fatal(err)
	}
	if scanned != original {
		t.Fatalf("sql round trip = %s", scanned)
	}

	var zero ID
	zeroJSON, err := json.Marshal(zero)
	if err != nil || string(zeroJSON) != "null" {
		t.Fatalf("zero JSON = %s, %v", zeroJSON, err)
	}
	if sqlValue, err := zero.Value(); err != nil || sqlValue != nil {
		t.Fatalf("zero SQL value = %v, %v", sqlValue, err)
	}
}

func TestIDOrdering(t *testing.T) {
	t.Parallel()

	first := MustParseID("run_0000000003e870008000000000000000")
	second := MustParseID("run_0000000003e870008000000000000001")
	otherKind := MustParseID("task_0000000003e870008000000000000000")

	if !first.Less(second) || first.Compare(second) >= 0 {
		t.Fatalf("same-kind ordering failed")
	}
	if strings.Compare(first.String(), second.String()) >= 0 {
		t.Fatalf("string ordering failed")
	}
	if second.Compare(otherKind) >= 0 {
		t.Fatalf("kind ordering failed")
	}
}

func TestZeroIDIsInvalid(t *testing.T) {
	t.Parallel()

	var identifier ID
	if identifier.String() != "" || !identifier.IsZero() || identifier.Valid() {
		t.Fatalf("zero ID state is inconsistent")
	}
	if !errors.Is(identifier.Validate(), ErrInvalidID) {
		t.Fatalf("Validate() error = %v", identifier.Validate())
	}
}
