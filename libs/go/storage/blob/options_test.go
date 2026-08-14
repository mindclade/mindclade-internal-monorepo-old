// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package blob

import "testing"

func TestListOptionsNormalized(t *testing.T) {
	normalized, err := (ListOptions{Prefix: "runs/"}).Normalized()
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Limit != DefaultListLimit {
		t.Fatalf("Limit = %d, want %d", normalized.Limit, DefaultListLimit)
	}
}

func TestListOptionsRejectNonCanonicalPrefixAndCursor(t *testing.T) {
	tests := []ListOptions{
		{Prefix: " runs/"},
		{Prefix: "runs//"},
		{Prefix: "runs/../"},
		{Prefix: "runs\\"},
		{Prefix: "runs/", Cursor: "other/result.cif"},
		{Cursor: "invalid/../cursor"},
	}
	for _, options := range tests {
		if _, err := options.Normalized(); err == nil {
			t.Fatalf("Normalized(%#v) returned nil", options)
		}
	}
}

func TestPutOptionsRejectControlContentType(t *testing.T) {
	if err := (PutOptions{ContentType: "text/plain\nmalicious"}).Validate(); err == nil {
		t.Fatal("PutOptions.Validate() returned nil")
	}
}
