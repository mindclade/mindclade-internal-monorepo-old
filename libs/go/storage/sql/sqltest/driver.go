// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package sqltest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
)

// QueryFunc handles a database/sql query after argument conversion.
type QueryFunc func(context.Context, string, []driver.NamedValue) (driver.Rows, error)

// ExecFunc handles a database/sql execution after argument conversion.
type ExecFunc func(context.Context, string, []driver.NamedValue) (driver.Result, error)

// PingFunc handles database/sql health probes.
type PingFunc func(context.Context) error

// State controls the deterministic driver. Configure callbacks before Open and
// do not mutate them while the returned database is in use.
type State struct {
	Begins        atomic.Int64
	Commits       atomic.Int64
	Rollbacks     atomic.Int64
	Queries       atomic.Int64
	Executions    atomic.Int64
	Pings         atomic.Int64
	BeginError    error
	CommitError   error
	RollbackError error
	Query         QueryFunc
	Exec          ExecFunc
	Ping          PingFunc
}

type Driver struct{ State *State }
type connection struct{ state *State }
type transaction struct{ state *State }

var sequence atomic.Uint64

func Open(state *State) (*sql.DB, error) {
	if state == nil {
		return nil, errors.New("sqltest: nil state")
	}
	name := fmt.Sprintf("mindclade-sqltest-%d", sequence.Add(1))
	sql.Register(name, Driver{State: state})
	return sql.Open(name, "")
}

func (value Driver) Open(string) (driver.Conn, error) {
	if value.State == nil {
		return nil, errors.New("sqltest: nil state")
	}
	return &connection{state: value.State}, nil
}

func (value *connection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("sqltest: statements unsupported")
}

func (value *connection) Close() error { return nil }

func (value *connection) Begin() (driver.Tx, error) {
	return value.BeginTx(context.Background(), driver.TxOptions{})
}

func (value *connection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	value.state.Begins.Add(1)
	if value.state.BeginError != nil {
		return nil, value.state.BeginError
	}
	return &transaction{state: value.state}, nil
}

func (value *connection) QueryContext(ctx context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
	value.state.Queries.Add(1)
	if value.state.Query == nil {
		return nil, errors.New("sqltest: queries unsupported")
	}
	return value.state.Query(ctx, query, cloneArguments(arguments))
}

func (value *connection) ExecContext(ctx context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	value.state.Executions.Add(1)
	if value.state.Exec == nil {
		return nil, errors.New("sqltest: executions unsupported")
	}
	return value.state.Exec(ctx, query, cloneArguments(arguments))
}

func (value *connection) Ping(ctx context.Context) error {
	value.state.Pings.Add(1)
	if value.state.Ping == nil {
		return nil
	}
	return value.state.Ping(ctx)
}

func (value *transaction) Commit() error {
	value.state.Commits.Add(1)
	return value.state.CommitError
}

func (value *transaction) Rollback() error {
	value.state.Rollbacks.Add(1)
	return value.state.RollbackError
}

func cloneArguments(values []driver.NamedValue) []driver.NamedValue {
	return append([]driver.NamedValue(nil), values...)
}

// Rows is a deterministic driver.Rows implementation.
type Rows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

// NewRows constructs rows and defensively copies both columns and values.
func NewRows(columns []string, values ...[]driver.Value) *Rows {
	rows := &Rows{columns: append([]string(nil), columns...), values: make([][]driver.Value, len(values))}
	for index := range values {
		rows.values[index] = append([]driver.Value(nil), values[index]...)
	}
	return rows
}

func (rows *Rows) Columns() []string {
	if rows == nil {
		return nil
	}
	return append([]string(nil), rows.columns...)
}

func (*Rows) Close() error { return nil }

func (rows *Rows) Next(destination []driver.Value) error {
	if rows == nil || rows.index >= len(rows.values) {
		return io.EOF
	}
	current := rows.values[rows.index]
	rows.index++
	if len(destination) != len(current) {
		return fmt.Errorf("sqltest: destination has %d columns, row has %d", len(destination), len(current))
	}
	copy(destination, current)
	return nil
}

var (
	_ driver.Driver         = Driver{}
	_ driver.Conn           = (*connection)(nil)
	_ driver.ConnBeginTx    = (*connection)(nil)
	_ driver.QueryerContext = (*connection)(nil)
	_ driver.ExecerContext  = (*connection)(nil)
	_ driver.Pinger         = (*connection)(nil)
	_ driver.Rows           = (*Rows)(nil)
)
