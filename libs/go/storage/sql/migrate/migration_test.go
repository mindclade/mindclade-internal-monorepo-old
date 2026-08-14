// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package migrate_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"mindclade.internal/libs/go/storage/sql/migrate"
	"mindclade.internal/libs/go/storage/sql/sqltest"
)

func TestManifestSortsAndChecksums(t *testing.T) {
	manifest, err := migrate.NewManifest(
		migrate.Migration{Version: 2, Name: "add_jobs", Up: "ALTER TABLE runs ADD COLUMN job_id TEXT"},
		migrate.Migration{Version: 1, Name: "create_runs", Up: "CREATE TABLE runs (id TEXT PRIMARY KEY)"},
	)
	if err != nil {
		t.Fatal(err)
	}
	values := manifest.Migrations()
	if len(values) != 2 || values[0].Version != 1 || len(values[0].Checksum()) != 64 {
		t.Fatalf("unexpected manifest: %#v", values)
	}
}

func TestRunnerAppliesPendingMigration(t *testing.T) {
	manifest, err := migrate.NewManifest(migrate.Migration{Version: 1, Name: "create_runs", Up: "CREATE TABLE runs (id TEXT PRIMARY KEY)"})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := migrate.NewRunner(manifest, migrate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	state := &sqltest.State{}
	state.Query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return sqltest.NewRows([]string{"version", "name", "checksum", "applied_at"}), nil
	}
	state.Exec = func(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
		if strings.TrimSpace(query) == "" {
			t.Fatal("empty query")
		}
		return driver.RowsAffected(1), nil
	}
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	plan, err := runner.Apply(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Applied) != 1 || plan.Applied[0].Version != 1 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if state.Begins.Load() != 1 || state.Commits.Load() != 1 || state.Rollbacks.Load() != 0 {
		t.Fatalf("transaction counts: begin=%d commit=%d rollback=%d", state.Begins.Load(), state.Commits.Load(), state.Rollbacks.Load())
	}
}

func TestRunnerRejectsChecksumDrift(t *testing.T) {
	migration := migrate.Migration{Version: 1, Name: "create_runs", Up: "CREATE TABLE runs (id TEXT PRIMARY KEY)"}
	manifest, _ := migrate.NewManifest(migration)
	runner, _ := migrate.NewRunner(manifest, migrate.Options{})
	state := &sqltest.State{
		Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			return sqltest.NewRows([]string{"version", "name", "checksum", "applied_at"}, []driver.Value{int64(1), "create_runs", strings.Repeat("0", 64), time.Now()}), nil
		},
		Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
			return driver.RowsAffected(1), nil
		},
	}
	db, _ := sqltest.Open(state)
	defer db.Close()
	if _, err := runner.Plan(context.Background(), db); err == nil {
		t.Fatal("expected checksum drift")
	}
}
