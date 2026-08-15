// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package sqltest

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
)

func TestTransactions(t *testing.T) {
	state := &State{}
	database, err := Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	transaction, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	if state.Begins.Load() != 1 || state.Commits.Load() != 1 {
		t.Fatalf("begins=%d commits=%d", state.Begins.Load(), state.Commits.Load())
	}
}

func TestQueryExecAndPing(t *testing.T) {
	state := &State{
		Query: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
			if query != "SELECT value" || len(arguments) != 1 || arguments[0].Value != "key" {
				t.Fatalf("query=%q arguments=%#v", query, arguments)
			}
			return NewRows([]string{"value"}, []driver.Value{"result"}), nil
		},
		Exec: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
			if query != "DELETE value" || len(arguments) != 1 {
				t.Fatalf("query=%q arguments=%#v", query, arguments)
			}
			return driver.RowsAffected(1), nil
		},
	}
	database, err := Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var value string
	if err := database.QueryRowContext(context.Background(), "SELECT value", "key").Scan(&value); err != nil || value != "result" {
		t.Fatalf("Scan() = %q, %v", value, err)
	}
	result, err := database.ExecContext(context.Background(), "DELETE value", "key")
	if err != nil {
		t.Fatal(err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("RowsAffected() = %d, %v", affected, err)
	}
	if err := database.PingContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if state.Queries.Load() != 1 || state.Executions.Load() != 1 || state.Pings.Load() != 1 {
		t.Fatalf("queries=%d executions=%d pings=%d", state.Queries.Load(), state.Executions.Load(), state.Pings.Load())
	}
}

func TestConfiguredFailures(t *testing.T) {
	sentinel := errors.New("sentinel")
	state := &State{BeginError: sentinel, Ping: func(context.Context) error { return sentinel }}
	database, err := Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.BeginTx(context.Background(), nil); !errors.Is(err, sentinel) {
		t.Fatalf("BeginTx() = %v", err)
	}
	if err := database.PingContext(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("PingContext() = %v", err)
	}
}

func TestRowsRejectMismatchedDestination(t *testing.T) {
	rows := NewRows([]string{"one", "two"}, []driver.Value{"one"})
	if err := rows.Next(make([]driver.Value, 2)); err == nil {
		t.Fatal("Rows.Next() returned nil")
	}
}
