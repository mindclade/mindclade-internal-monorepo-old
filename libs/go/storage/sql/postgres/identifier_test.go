// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package postgres

import "testing"

func TestValidQualifiedIdentifier(t *testing.T) {
	valid := []string{"events", "mindclade_events", "control.events", "_private"}
	for _, value := range valid {
		if !ValidQualifiedIdentifier(value) {
			t.Fatalf("expected valid: %q", value)
		}
	}
	invalid := []string{"", " Events", "public.Events", "a.b.c", "a..b", "1table", "table-name", "table;drop"}
	for _, value := range invalid {
		if ValidQualifiedIdentifier(value) {
			t.Fatalf("expected invalid: %q", value)
		}
	}
}
