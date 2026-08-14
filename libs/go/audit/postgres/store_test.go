// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package postgres

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"mindclade.internal/libs/go/audit"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/storage/sql/sqltest"
)

func testEvent(t *testing.T) audit.Event {
	t.Helper()
	factory, err := audit.NewFactory()
	if err != nil {
		t.Fatal(err)
	}
	actor, err := audit.NewSystemActor("registry")
	if err != nil {
		t.Fatal(err)
	}
	target, err := audit.NewTarget("model_release", audit.WithTargetName("clade-1"))
	if err != nil {
		t.Fatal(err)
	}
	event, err := factory.Create(audit.MustParseAction("models.release.update"), actor, target, audit.OutcomeSucceeded)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestRecordInsertsImmutableEvent(t *testing.T) {
	state := &sqltest.State{Exec: func(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
		if !strings.Contains(query, "ON CONFLICT (event_id) DO NOTHING") || len(args) != 14 {
			t.Fatalf("query=%q args=%d", query, len(args))
		}
		return driver.RowsAffected(1), nil
	}}
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(context.Background(), testEvent(t)); err != nil {
		t.Fatal(err)
	}
}

func TestRecordAcceptsExactReplay(t *testing.T) {
	event := testEvent(t)
	payload, _ := event.MarshalJSON()
	digest := "sha256:" + strings.Repeat("0", 64)
	_ = payload
	state := &sqltest.State{}
	state.Exec = func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		return driver.RowsAffected(0), nil
	}
	state.Query = func(_ context.Context, _ string, args []driver.NamedValue) (driver.Rows, error) {
		// Use the digest passed to INSERT to ensure the duplicate is exact.
		_ = digest
		// The test driver cannot observe prior Exec arguments here, so recompute.
		body, _ := event.MarshalJSON()
		return sqltest.NewRows([]string{"event_digest"}, []driver.Value{auditDigest(body)}), nil
	}
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(context.Background(), event); err != nil {
		t.Fatal(err)
	}
}

func auditDigest(value []byte) string {
	// local helper avoids exporting implementation detail from the adapter.
	return fmtDigest(value)
}

func fmtDigest(value []byte) string {
	return identifiers.SHA256(value).String()
}
