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

	"go.mindclade.dev/libs/go/coordination/cursor"
	"go.mindclade.dev/libs/go/storage/sql/sqltest"
)

func TestDDL(t *testing.T) {
	ddl, err := DDL("control.cursors")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ddl, "PRIMARY KEY(namespace,name)") {
		t.Fatal("missing key")
	}
}

func TestDeleteReturnsRowsAffectedError(t *testing.T) {
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
	store, err := New(db, "control.cursors")
	if err != nil {
		t.Fatal(err)
	}
	key, err := cursor.NewKey("control", "checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Delete(context.Background(), key, 1); !errors.Is(err, want) {
		t.Fatalf("Delete() error = %v, want %v", err, want)
	}
}

type failingResult struct{ err error }

func (failingResult) LastInsertId() (int64, error) { return 0, nil }
func (result failingResult) RowsAffected() (int64, error) {
	return 0, result.err
}
