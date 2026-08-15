// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package requestmeta

import (
	"testing"
)

func TestMetadataDefaultsAndMerge(t *testing.T) {
	t.Parallel()

	requestID := MustParseRequestID(testRequestIDText)
	metadata, err := New(requestID)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.CorrelationID.String() != requestID.String() {
		t.Fatalf("correlation ID = %q", metadata.CorrelationID.String())
	}

	operation := MustParseOperation("runs.Repository.Create")
	causationID, err := ParseCausationID("event-123")
	if err != nil {
		t.Fatal(err)
	}
	merged := metadata.Merge(Metadata{Operation: operation, CausationID: causationID})
	if merged.Operation != operation || merged.CausationID != causationID {
		t.Fatalf("Merge() = %#v", merged)
	}
	if err := merged.Validate(); err != nil {
		t.Fatal(err)
	}
	fields := merged.Fields()
	if fields["request_id"] != requestID.String() || fields["operation"] != operation.String() {
		t.Fatalf("Fields() = %#v", fields)
	}
}

func TestOperationAndPropagationTokens(t *testing.T) {
	t.Parallel()

	if _, err := ParseOperation("models.release.promote"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseOperation("bad operation"); err == nil {
		t.Fatal("operation with whitespace accepted")
	}
	if _, err := ParseCorrelationID("trace:abc-123"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCorrelationID("line\nbreak"); err == nil {
		t.Fatal("control character accepted")
	}
}
