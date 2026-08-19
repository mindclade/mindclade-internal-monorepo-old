// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package persistence

import (
	"testing"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/libs/go/storage/sql/migrate"
)

// A migration runner with no pool would let a process report the migration
// capability and never be able to apply one.
func TestMigrationsWithoutADatabaseFailClosed(t *testing.T) {
	manifest, err := migrate.NewManifest(migrate.Migration{
		Version: 1, Name: "example", Up: "CREATE TABLE IF NOT EXISTS example (id text primary key);",
	})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := migrate.NewRunner(manifest, migrate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	builder, err := production.NewBuilder("test", production.RoleAPI)
	if err != nil {
		t.Fatal(err)
	}
	err = SQL{Migrations: runner}.Register(builder)
	if err == nil || faults.ReasonOf(err) != "migrations_without_database" {
		t.Fatalf("err=%v", err)
	}
}

func TestEmptySQLProvidesNothing(t *testing.T) {
	if capabilities := (SQL{}).Capabilities(); len(capabilities) != 0 {
		t.Fatalf("capabilities=%v", capabilities)
	}
}
