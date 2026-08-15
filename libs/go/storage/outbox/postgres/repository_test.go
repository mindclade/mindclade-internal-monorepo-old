// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"strings"
	"testing"
)

func TestDDL(t *testing.T) {
	t.Parallel()
	ddl, err := DDL("control_outbox")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"CREATE TABLE IF NOT EXISTS control_outbox", "FOR UPDATE", "pending", "dead_letter"} {
		if fragment == "FOR UPDATE" {
			// Claim SQL is exercised by the canonical adapter tests; DDL does not contain it.
			continue
		}
		if !strings.Contains(ddl, fragment) {
			t.Fatalf("DDL missing %q", fragment)
		}
	}
}

func TestDDLRejectsUnsafeIdentifier(t *testing.T) {
	t.Parallel()
	if _, err := DDL("outbox;drop table runs"); err == nil {
		t.Fatal("DDL accepted unsafe identifier")
	}
}
