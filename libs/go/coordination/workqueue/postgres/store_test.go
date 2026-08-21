// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/sql/sqltest"
)

func TestDDL(t *testing.T) {
	ddl, err := DDL("control.work_items")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ddl, "SKIP LOCKED") && strings.Contains(ddl, "CREATE TABLE") == false {
		t.Fatal("invalid DDL")
	}
}

func TestTerminalRetentionDDLIsAdditiveAndPartial(t *testing.T) {
	ddl, err := TerminalRetentionDDL("control.work_items")
	if err != nil {
		t.Fatal(err)
	}
	want := "CREATE INDEX work_items_terminal_retention_idx ON control.work_items(queue,completed_at,item_id) WHERE state IN ('completed','failed','cancelled');"
	if ddl != want {
		t.Fatalf("TerminalRetentionDDL() = %q, want %q", ddl, want)
	}
	if _, err := TerminalRetentionDDL("control.work-items"); err == nil {
		t.Fatal("TerminalRetentionDDL accepted an invalid table name")
	}
}

func TestLookupRejectsMalformedRequestMetadata(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	id, err := identifiers.NewIDAt(workqueue.ItemIDKind, now)
	if err != nil {
		t.Fatal(err)
	}
	state := &sqltest.State{Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return sqltest.NewRows(
			[]string{"item_id", "queue", "payload", "priority", "available_at", "max_attempts", "created_at", "request_metadata", "state", "attempts", "fence", "updated_at", "completed_at", "result_content_type", "result_payload", "last_error", "claim_token", "claim_owner", "claimed_at", "claim_expires_at"},
			[]driver.Value{id.String(), "default", []byte(`{"job":"test"}`), int64(0), now, int64(3), now, []byte(`{"request_id":`), string(workqueue.StatePending), int64(0), int64(0), now, nil, nil, nil, nil, nil, nil, nil, nil},
		), nil
	}}
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := New(db, "control.work_items")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Lookup(context.Background(), id); err == nil || !strings.Contains(err.Error(), "decode request metadata") {
		t.Fatalf("Lookup() error = %v, want malformed metadata error", err)
	}
}

func TestCancelReturnsRowsAffectedError(t *testing.T) {
	t.Parallel()
	want := errors.New("rows affected unavailable")
	state := &sqltest.State{Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		return failingResult{err: want}, nil
	}}
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := New(db, "control.work_items")
	if err != nil {
		t.Fatal(err)
	}
	id, err := identifiers.NewID(workqueue.ItemIDKind)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Cancel(context.Background(), id, "cancelled by test", time.Now().UTC()); !errors.Is(err, want) {
		t.Fatalf("Cancel() error = %v, want %v", err, want)
	}
}

func TestPruneTerminalIsBoundedQueueScopedAndSkipLocked(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	var query string
	var arguments []driver.NamedValue
	state := &sqltest.State{Exec: func(_ context.Context, value string, values []driver.NamedValue) (driver.Result, error) {
		query = value
		arguments = values
		return driver.RowsAffected(7), nil
	}}
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := New(db, "control.work_items")
	if err != nil {
		t.Fatal(err)
	}
	pruned, err := store.PruneTerminal(context.Background(), workqueue.PruneRequest{
		Queue: "control-plane/maintenance", CompletedBefore: cutoff, Limit: 17,
	})
	if err != nil || pruned != 7 {
		t.Fatalf("PruneTerminal() = %d, %v, want 7, nil", pruned, err)
	}
	for _, fragment := range []string{
		"queue=$1", "state IN ('completed','failed','cancelled')", "completed_at < $2",
		"ORDER BY completed_at,item_id", "FOR UPDATE SKIP LOCKED", "LIMIT $3", "DELETE FROM control.work_items",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("prune query does not contain %q: %s", fragment, query)
		}
	}
	if len(arguments) != 3 || arguments[0].Value != "control-plane/maintenance" ||
		arguments[1].Value != cutoff || arguments[2].Value != int64(17) {
		t.Fatalf("prune arguments = %#v", arguments)
	}
}

func TestPruneTerminalRejectsInvalidRequestWithoutQuery(t *testing.T) {
	t.Parallel()
	state := &sqltest.State{Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		t.Fatal("invalid request reached the database")
		return nil, nil
	}}
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := New(db, "control.work_items")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PruneTerminal(context.Background(), workqueue.PruneRequest{
		Queue: "control-plane/maintenance", CompletedBefore: time.Now().UTC(), Limit: workqueue.MaximumPruneLimit + 1,
	}); !errors.Is(err, workqueue.ErrInvalidRequest) {
		t.Fatalf("PruneTerminal() error = %v, want invalid request", err)
	}
}

type failingResult struct{ err error }

func (failingResult) LastInsertId() (int64, error) { return 0, nil }
func (result failingResult) RowsAffected() (int64, error) {
	return 0, result.err
}
