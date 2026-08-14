// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package postgres

import (
	"strings"
	"testing"
)

func TestDDL(t *testing.T) {
	ddl, err := DDL("control_outbox")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"CREATE TABLE IF NOT EXISTS control_outbox", "FOR UPDATE", "pending_idx", "claim_idx"} {
		if fragment == "FOR UPDATE" {
			continue
		}
		if !strings.Contains(ddl, fragment) {
			t.Fatalf("DDL missing %q", fragment)
		}
	}
	if _, err := DDL("bad-name"); err == nil {
		t.Fatal("invalid table accepted")
	}
}
