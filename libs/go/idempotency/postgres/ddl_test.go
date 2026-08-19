// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"strings"
	"testing"
)

func TestDDLNamesTheConfiguredTable(t *testing.T) {
	statement, err := DDL("billing.idempotency")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statement, "CREATE TABLE IF NOT EXISTS billing.idempotency (") {
		t.Fatalf("statement=%s", statement)
	}
	// The index is derived from the table so two tables in one database cannot
	// collide on it.
	if !strings.Contains(statement, "billing_idempotency_expiry_idx") {
		t.Fatalf("statement=%s", statement)
	}
	if strings.Contains(statement, DefaultTable) {
		t.Fatalf("configured table was ignored: %s", statement)
	}
}

func TestDDLMatchesTheDefaultTableUsedByNew(t *testing.T) {
	statement, err := DDL(DefaultTable)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statement, "CREATE TABLE IF NOT EXISTS "+DefaultTable+" (") {
		t.Fatalf("statement=%s", statement)
	}
}

func TestDDLRejectsInvalidTableNames(t *testing.T) {
	for _, table := range []string{"", "  ", "Idempotency", "1records", "records;DROP TABLE x", "a..b"} {
		if _, err := DDL(table); err == nil {
			t.Fatalf("accepted invalid table %q", table)
		}
	}
}

func TestIndexNameTruncatesToThePostgresIdentifierLimit(t *testing.T) {
	name := indexName(strings.Repeat("a", 200), "expiry_idx")
	if len(name) > maximumPostgresIdentifierBytes {
		t.Fatalf("len=%d name=%s", len(name), name)
	}
	if !strings.HasSuffix(name, "_expiry_idx") {
		t.Fatalf("name=%s", name)
	}
}
