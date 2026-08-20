// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Ingestion's durability property, which spans the domain state machine and the commit point
// rather than living in either.
//
// control/ingestion owns when a stage may change state and when a cursor may advance;
// storage/sql/transaction owns whether those two land together. A cursor that advances
// without the stage that justified it re-reads nothing and silently drops a source window,
// which is the one ingestion failure that leaves no error behind to find later.
package tests

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"go.mindclade.dev/libs/go/coordination/outbox/outboxtest"
	"go.mindclade.dev/libs/go/storage/sql/transaction"

	"go.mindclade.dev/control/ingestion"
)

func TestOnlyATerminalSuccessAdvancesTheCursor(t *testing.T) {
	// The state machine and the cursor rule have to agree. Enumerated rather than spot-checked
	// so a new State added to one and not the other shows up here.
	for _, testCase := range []struct {
		from    ingestion.State
		to      ingestion.State
		allowed bool
		commits bool
	}{
		{ingestion.StatePending, ingestion.StateRunning, true, false},
		{ingestion.StatePending, ingestion.StateCanceled, true, false},
		{ingestion.StateRunning, ingestion.StateCompleted, true, true},
		{ingestion.StateRunning, ingestion.StateFailed, true, false},
		{ingestion.StateRunning, ingestion.StateCanceled, true, false},
		// Backwards, skipping, and out of a terminal state are all refused.
		{ingestion.StateRunning, ingestion.StatePending, false, false},
		{ingestion.StatePending, ingestion.StateCompleted, false, false},
		{ingestion.StateCompleted, ingestion.StateRunning, false, false},
		{ingestion.StateFailed, ingestion.StateRunning, false, false},
		{ingestion.StateCanceled, ingestion.StatePending, false, false},
	} {
		if got := ingestion.CanTransition(testCase.from, testCase.to); got != testCase.allowed {
			t.Fatalf("CanTransition(%q, %q) = %v, want %v",
				testCase.from, testCase.to, got, testCase.allowed)
		}
		if testCase.commits && !testCase.to.Terminal() {
			t.Fatalf("%q advances the cursor but is not terminal", testCase.to)
		}
	}
}

func TestACursorAdvanceCommitsWithItsStageTransition(t *testing.T) {
	state, db, store, fake := harness(t)
	current := ingestion.Cursor{Value: "2026-08-01T00:00:00Z", Sequence: 41}
	next := ingestion.Cursor{Value: "2026-08-02T00:00:00Z", Sequence: 42}

	if !current.CanAdvance(next) {
		t.Fatal("fixture cursors do not advance")
	}
	message := outboxtest.Message(t, fake.Now(), "ingestion.window.published", []byte("payload"))

	err := transaction.RunVoid(context.Background(), db, transaction.Options{},
		func(ctx context.Context, tx *sql.Tx) error {
			if !ingestion.CanTransition(ingestion.StateRunning, ingestion.StateCompleted) {
				t.Fatal("running -> completed refused")
			}
			if _, err := tx.ExecContext(ctx,
				"UPDATE ingestion_source SET cursor_sequence = $1", next.Sequence); err != nil {
				return err
			}
			return store.Append(ctx, message)
		})
	if err != nil {
		t.Fatal(err)
	}
	if state.Commits.Load() != 1 || state.Begins.Load() != 1 {
		t.Fatalf("begins=%d commits=%d, want one of each",
			state.Begins.Load(), state.Commits.Load())
	}
}

func TestACursorNeverAdvancesBackwardsOrRepeats(t *testing.T) {
	// The replay-safety rule. Ingestion is resumable, so the same window is offered again
	// after a restart; CanAdvance is what stops a retry rewinding a source that already moved.
	at := ingestion.Cursor{Value: "2026-08-02T00:00:00Z", Sequence: 42}
	for name, candidate := range map[string]ingestion.Cursor{
		"backwards": {Value: "2026-08-01T00:00:00Z", Sequence: 41},
		"identical": at,
		"unset":     {},
		"novalue":   {Value: "", Sequence: 43},
		"zeroseq":   {Value: "2026-08-03T00:00:00Z", Sequence: 0},
	} {
		if at.CanAdvance(candidate) {
			t.Fatalf("cursor advanced to the %s candidate %+v", name, candidate)
		}
	}
}

func TestAnInvalidCursorIsRejectedBeforeTheCommit(t *testing.T) {
	state, db, _, _ := harness(t)
	invalid := ingestion.Cursor{Value: "", Sequence: 0}
	sentinel := errors.New("cursor rejected")

	err := transaction.RunVoid(context.Background(), db, transaction.Options{},
		func(ctx context.Context, tx *sql.Tx) error {
			if err := invalid.Validate(); err != nil {
				return sentinel
			}
			_, err := tx.ExecContext(ctx, "UPDATE ingestion_source SET cursor_sequence = $1", 1)
			return err
		})
	if !errors.Is(err, sentinel) {
		t.Fatalf("RunVoid() = %v, want the validation failure", err)
	}
	if state.Commits.Load() != 0 {
		t.Fatal("an invalid cursor reached a commit")
	}
}
