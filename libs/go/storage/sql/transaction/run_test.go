// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package transaction

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"go.mindclade.dev/libs/go/faults"
)

type testState struct {
	begins, commits, rollbacks atomic.Int64
	commitErr, rollbackErr     error
}
type testDriver struct{ state *testState }
type testConn struct{ state *testState }
type testTx struct{ state *testState }

func (driver testDriver) Open(string) (driver.Conn, error) {
	return &testConn{state: driver.state}, nil
}
func (conn *testConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (conn *testConn) Close() error                        { return nil }
func (conn *testConn) Begin() (driver.Tx, error) {
	return conn.BeginTx(context.Background(), driver.TxOptions{})
}
func (conn *testConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	conn.state.begins.Add(1)
	return &testTx{state: conn.state}, nil
}
func (tx *testTx) Commit() error   { tx.state.commits.Add(1); return tx.state.commitErr }
func (tx *testTx) Rollback() error { tx.state.rollbacks.Add(1); return tx.state.rollbackErr }

var driverSequence atomic.Uint64

func openDB(t *testing.T, state *testState) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("transaction-test-%d", driverSequence.Add(1))
	sql.Register(name, testDriver{state: state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestRunCommit(t *testing.T) {
	t.Parallel()
	state := &testState{}
	db := openDB(t, state)
	result, err := Run(context.Background(), db, Options{}, func(ctx context.Context, tx *sql.Tx) (string, error) {
		if current, ok := FromContext(ctx); !ok || current != tx {
			t.Fatal("transaction missing from context")
		}
		return "ok", nil
	})
	if err != nil || result != "ok" {
		t.Fatalf("Run=%q %v", result, err)
	}
	if state.commits.Load() != 1 || state.rollbacks.Load() != 0 {
		t.Fatalf("state=%+v", state)
	}
}
func TestRunRollback(t *testing.T) {
	t.Parallel()
	state := &testState{}
	db := openDB(t, state)
	sentinel := errors.New("callback")
	_, err := Run(context.Background(), db, Options{}, func(context.Context, *sql.Tx) (struct{}, error) { return struct{}{}, sentinel })
	if !errors.Is(err, sentinel) || state.rollbacks.Load() != 1 {
		t.Fatalf("error=%v rollbacks=%d", err, state.rollbacks.Load())
	}
}
func TestRunPanicRollsBack(t *testing.T) {
	t.Parallel()
	state := &testState{}
	db := openDB(t, state)
	defer func() {
		if recover() == nil {
			t.Fatal("panic not propagated")
		}
		if state.rollbacks.Load() != 1 {
			t.Fatalf("rollbacks=%d", state.rollbacks.Load())
		}
	}()
	_, _ = Run(context.Background(), db, Options{}, func(context.Context, *sql.Tx) (struct{}, error) { panic("boom") })
}
func TestCommitFailure(t *testing.T) {
	t.Parallel()
	state := &testState{commitErr: driver.ErrBadConn}
	db := openDB(t, state)
	_, err := Run(context.Background(), db, Options{}, func(context.Context, *sql.Tx) (struct{}, error) { return struct{}{}, nil })
	if err == nil {
		t.Fatal("expected error")
	}
}

var _ sync.Locker = (*sync.Mutex)(nil)

func TestRunQualifiesCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, nil, Options{}, func(context.Context, *sql.Tx) (struct{}, error) {
		return struct{}{}, nil
	})
	if !faults.IsCode(err, faults.CodeInvalidArgument) {
		// Structural request validation intentionally precedes context state.
		t.Fatalf("nil beginner error = %v", err)
	}

	state := &testState{}
	db := openDB(t, state)
	_, err = Run(ctx, db, Options{}, func(context.Context, *sql.Tx) (struct{}, error) {
		return struct{}{}, nil
	})
	if !errors.Is(err, context.Canceled) || !faults.IsCode(err, faults.CodeCanceled) {
		t.Fatalf("canceled context error = %v", err)
	}
	if state.begins.Load() != 0 {
		t.Fatalf("begins = %d", state.begins.Load())
	}
}
