// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"strings"
	"testing"
)

// The DDL and the store must name the same tables. A migration applied against
// a fixed name next to a renamed store is how a schema and its reader stop
// describing the same table without anything failing at startup.
func TestDDLCoversTheConfiguredTables(t *testing.T) {
	t.Parallel()
	store, err := New(nil)
	if store != nil || err == nil {
		t.Fatal("a nil database was accepted")
	}
	statements, err := DDL(DefaultDescriptorTable, DefaultReleaseTable, DefaultEvidenceGraphTable)
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 3 {
		t.Fatalf("statements=%d", len(statements))
	}
	for index, table := range []string{DefaultDescriptorTable, DefaultReleaseTable, DefaultEvidenceGraphTable} {
		if !strings.Contains(statements[index], "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("statement %d does not create %s", index, table)
		}
	}
}

func TestEvidenceLedgerDDLCoversTheConfiguredTables(t *testing.T) {
	t.Parallel()
	tables := []string{
		DefaultEvidenceClaimTable,
		DefaultEvidenceVerificationTable,
		DefaultEligibilityDecisionTable,
		DefaultEligibilityRevocationTable,
	}
	statements, err := EvidenceLedgerDDL(tables[0], tables[1], tables[2], tables[3])
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != len(tables) {
		t.Fatalf("statements=%d", len(statements))
	}
	for index, table := range tables {
		if !strings.Contains(statements[index], "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("statement %d does not create %s", index, table)
		}
	}
}

// Table names reach string-formatted SQL, so a rejected name is the boundary
// that keeps configuration from becoming an injection point.
func TestDDLRefusesAnUnsafeTableName(t *testing.T) {
	t.Parallel()
	for _, table := range []string{
		"", "Registry", "registry-models", "registry models",
		"models; DROP TABLE mindclade_audit_records", "1models", ".models", "models.",
	} {
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			if _, err := DescriptorDDL(table); err == nil {
				t.Fatalf("accepted table name %q", table)
			}
			if _, err := New(nil, WithDescriptorTable(table)); err == nil {
				t.Fatalf("store accepted table name %q", table)
			}
			if _, err := EvidenceLedgerDDL(table, DefaultEvidenceVerificationTable, DefaultEligibilityDecisionTable, DefaultEligibilityRevocationTable); err == nil {
				t.Fatalf("evidence ledger accepted table name %q", table)
			}
		})
	}
}

// PostgreSQL truncates identifiers at NAMEDATALEN-1, so index names derived
// from a long table must truncate here rather than collide server-side.
func TestIndexNamesStayWithinThePostgresIdentifierLimit(t *testing.T) {
	t.Parallel()
	long := "s" + strings.Repeat("x", 62) + "." + "t" + strings.Repeat("y", 62)
	for _, suffix := range []string{"model_idx", "lifecycle_idx", "channel_idx", "subject_idx"} {
		name := indexName(long, suffix)
		if len(name) > maximumPostgresIdentifierBytes {
			t.Fatalf("index name %q is %d bytes", name, len(name))
		}
		if !strings.HasSuffix(name, suffix) {
			t.Fatalf("index name %q lost its suffix", name)
		}
	}
}

// The two indexes on one table must not collapse to the same name after
// truncation, which is what a shared prefix plus a truncated suffix would do.
func TestIndexNamesOnOneTableStayDistinct(t *testing.T) {
	t.Parallel()
	long := "t" + strings.Repeat("y", 80)
	if indexName(long, "model_idx") == indexName(long, "lifecycle_idx") {
		t.Fatal("two indexes on one table truncated to the same name")
	}
}
