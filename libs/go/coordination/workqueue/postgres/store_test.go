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

type failingResult struct{ err error }

func (failingResult) LastInsertId() (int64, error) { return 0, nil }
func (result failingResult) RowsAffected() (int64, error) {
	return 0, result.err
}
