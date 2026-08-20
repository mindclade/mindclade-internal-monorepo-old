// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// The mutation-and-publication contract, which GO_FOUNDATION_CONSUMPTION.md states as:
//
//	SQL transaction -> domain mutation -> audit record -> outbox insert -> commit
//	                -> asynchronous dispatcher publication
//
// It is the reason services/control_plane/internal/store/postgres/store.go resolves its
// executor per call from the context rather than binding one at construction: a repository
// that opened its own transaction could not be composed into this commit. That comment
// asserts the property; nothing asserted it. These tests do.
//
// This file is under services/, not beside any one package, because the property spans four:
// storage/sql/transaction owns the boundary, audit owns the record, coordination/outbox owns
// the insert, and the domain owns the mutation. None of them can observe it alone.
package tests

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/audit"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/coordination/outbox"
	outboxmemory "go.mindclade.dev/libs/go/coordination/outbox/memory"
	"go.mindclade.dev/libs/go/coordination/outbox/outboxtest"
	"go.mindclade.dev/libs/go/storage/sql/sqltest"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
)

// commitPoint is the smallest thing that behaves like a control-plane write: it mutates
// domain state, records an audit event, and enqueues the notification, all through whatever
// executor the context carries.
type commitPoint struct {
	mutated  bool
	audited  bool
	enqueued bool
}

func (c *commitPoint) apply(ctx context.Context, tx *sql.Tx, recorder audit.Recorder, store outbox.Store, message outbox.Message, fail error) error {
	if _, err := tx.ExecContext(ctx, "UPDATE ingestion_stage SET state = $1", "completed"); err != nil {
		return err
	}
	c.mutated = true
	if err := recorder.Record(ctx, audit.Event{}); err != nil {
		return err
	}
	c.audited = true
	if err := store.Append(ctx, message); err != nil {
		return err
	}
	c.enqueued = true
	// Injected after every effect has been applied. That is the interesting position: each
	// step reported success, so only the transaction can still undo them.
	return fail
}

func harness(t *testing.T) (*sqltest.State, *sql.DB, outbox.Store, *clock.FakeClock) {
	t.Helper()
	state := &sqltest.State{
		Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
			return driver.RowsAffected(1), nil
		},
	}
	db, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := outboxmemory.New()
	if err != nil {
		t.Fatal(err)
	}
	return state, db, store, clock.NewFake(time.Unix(1770000000, 0).UTC())
}

func TestMutationAuditAndOutboxShareOneCommit(t *testing.T) {
	state, db, store, fake := harness(t)
	point := &commitPoint{}
	message := outboxtest.Message(t, fake.Now(), "ingestion.stage.completed", []byte("payload"))
	recorder := audit.RecorderFunc(func(context.Context, audit.Event) error { return nil })

	err := transaction.RunVoid(context.Background(), db, transaction.Options{},
		func(ctx context.Context, tx *sql.Tx) error {
			return point.apply(ctx, tx, recorder, store, message, nil)
		})
	if err != nil {
		t.Fatal(err)
	}

	if !point.mutated || !point.audited || !point.enqueued {
		t.Fatalf("not every effect ran: %+v", point)
	}
	// One Begin and one Commit is the assertion. Three transactions here would also leave
	// every effect applied, and would still be wrong: a crash between them publishes a
	// notification for a mutation that never landed.
	if got := state.Begins.Load(); got != 1 {
		t.Fatalf("Begins = %d, want exactly 1", got)
	}
	if got := state.Commits.Load(); got != 1 {
		t.Fatalf("Commits = %d, want exactly 1", got)
	}
	if got := state.Rollbacks.Load(); got != 0 {
		t.Fatalf("Rollbacks = %d, want 0", got)
	}
}

func TestAFailedPublicationRollsBackTheMutation(t *testing.T) {
	state, db, store, fake := harness(t)
	point := &commitPoint{}
	message := outboxtest.Message(t, fake.Now(), "ingestion.stage.completed", []byte("payload"))
	recorder := audit.RecorderFunc(func(context.Context, audit.Event) error { return nil })
	sentinel := errors.New("publication refused")

	err := transaction.RunVoid(context.Background(), db, transaction.Options{},
		func(ctx context.Context, tx *sql.Tx) error {
			return point.apply(ctx, tx, recorder, store, message, sentinel)
		})
	if !errors.Is(err, sentinel) {
		t.Fatalf("RunVoid() = %v, want the injected failure", err)
	}

	if state.Commits.Load() != 0 {
		t.Fatalf("Commits = %d, want 0 -- a failed unit of work committed", state.Commits.Load())
	}
	if state.Rollbacks.Load() != 1 {
		t.Fatalf("Rollbacks = %d, want exactly 1", state.Rollbacks.Load())
	}
}

func TestTheCommitPointRefusesToNest(t *testing.T) {
	// A repository that opened its own transaction would nest one here. transaction.Run
	// refuses that outright rather than silently running the inner work in the outer
	// transaction, or worse, in a second one -- so the refusal is what keeps a repository
	// composable into the commit above.
	_, db, _, _ := harness(t)
	err := transaction.RunVoid(context.Background(), db, transaction.Options{},
		func(ctx context.Context, _ *sql.Tx) error {
			return transaction.RunVoid(ctx, db, transaction.Options{},
				func(context.Context, *sql.Tx) error { return nil })
		})
	if !errors.Is(err, transaction.ErrNested) {
		t.Fatalf("nested RunVoid() = %v, want ErrNested", err)
	}
}
