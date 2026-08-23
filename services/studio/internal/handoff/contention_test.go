// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Redemption's load-then-bind loop, driven directly.
//
// Everything in handoff_test.go needs a real PostgreSQL and SKIPS without
// STUDIO_TEST_DATABASE_URL, so the loop's own termination has never been under
// test in CI — which is how it shipped with no bound at all. These tests need
// no database: they hand the loop its two statements as closures and assert
// what the loop does with what they return.
package handoff

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func fixedStore() *Store {
	at := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	return &Store{ttl: DefaultTTL, outstandingCap: DefaultOutstandingCap, now: func() time.Time { return at }}
}

func unboundRow() handleRow {
	return handleRow{creator: "test-ci", resourceRef: []byte(`{"run":"123"}`)}
}

// THE REGRESSION GUARD.
//
// A bind that keeps losing must stop, and must stop after a fixed number of
// laps. Before this was bounded the loop re-entered Redeem by tail call, so a
// row that kept changing underneath it grew the goroutine stack until the
// runtime killed the whole process — and called materialize once per lap on
// the way there, orphaning a canvas document each time.
//
// The fake bind loses far more often than the bound allows, so the assertions
// below are on the LOOP's arithmetic and not on how the fake happens to be
// tuned.
func TestPersistentBindContentionIsBounded(t *testing.T) {
	s := fixedStore()

	var loads, materializations, binds int
	load := func(context.Context) (handleRow, error) {
		loads++
		return unboundRow(), nil
	}
	bind := func(context.Context, string) (string, error) {
		binds++
		if binds > 50 {
			// Deliberately reachable. An unbounded loop terminates HERE, at 51
			// laps, and reports success — which is what makes the failure
			// legible instead of a stack-overflow crash that takes the test
			// binary with it.
			return "doc-eventually", nil
		}
		return "", ErrNotFound
	}
	materialize := func(context.Context, string, json.RawMessage) (string, error) {
		materializations++
		return "doc-attempt", nil
	}

	var recorded []AuditEvent
	audit := func(_ context.Context, e AuditEvent) { recorded = append(recorded, e) }

	got, err := s.redeem(context.Background(), "handle-1", "test-alice",
		load, bind, allow, materialize, audit)

	if !errors.Is(err, ErrContended) {
		t.Fatalf("err = %v (redemption %+v), want ErrContended", err, got)
	}
	if binds != maxBindAttempts {
		t.Errorf("binds = %d, want %d", binds, maxBindAttempts)
	}
	// One more load than binds: the loop ends on a RECONCILING READ, so a row
	// that became bound to this principal on the last lap is answered with the
	// idempotent redirect rather than a 503.
	if loads != maxBindAttempts+1 {
		t.Errorf("loads = %d, want %d — the loop must end on a read", loads, maxBindAttempts+1)
	}
	// The cost that lands outside this process: a document materialized per lap
	// is a canvas document nothing will ever reference.
	if materializations != 1 {
		t.Errorf("materialize called %d times, want exactly 1; "+
			"each surplus call orphans a document", materializations)
	}
	// The one terminal branch that can have created a document must not be the
	// one branch missing from the append-only trail.
	if len(recorded) != 1 || recorded[0].Outcome != "contended" {
		t.Errorf("audit trail = %+v, want a single \"contended\" event", recorded)
	}
}

// The last lost bind must be reconciled by a read, not reported as contention.
//
// A lost bind means the row IS bound now — quite possibly to this same
// principal, which is the double-click this loop exists to serve. Answering
// 503 there would deny a caller the document they just created.
func TestBindLostOnTheFinalAttemptStillReturnsTheDocument(t *testing.T) {
	s := fixedStore()

	var loads int
	load := func(context.Context) (handleRow, error) {
		loads++
		if loads <= maxBindAttempts {
			return unboundRow(), nil
		}
		row := unboundRow()
		row.boundTo, row.isBound, row.docID = "test-alice", true, "doc-winner"
		return row, nil
	}

	got, err := s.redeem(context.Background(), "handle-1", "test-alice",
		load,
		func(context.Context, string) (string, error) { return "", ErrNotFound },
		allow, makeDocFn("doc-loser"), noAudit)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if got.DocID != "doc-winner" || got.FirstRedemption {
		t.Errorf("got = %+v, want the winner's document as an idempotent redemption", got)
	}
}

// Same shape, but the handle expired underneath: the honest answer is 404, and
// ErrContended would send the caller back to a permanently dead link.
func TestHandleExpiringDuringContentionIsNotFound(t *testing.T) {
	s := fixedStore()

	var loads int
	load := func(context.Context) (handleRow, error) {
		loads++
		if loads <= maxBindAttempts {
			return unboundRow(), nil
		}
		return handleRow{}, ErrNotFound
	}

	_, err := s.redeem(context.Background(), "handle-1", "test-alice",
		load,
		func(context.Context, string) (string, error) { return "", ErrNotFound },
		allow, makeDocFn("doc"), noAudit)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// The ordinary double-click: lose the bind once, then find the winner's
// binding. Bounding the loop must not break the case the loop exists for.
func TestLosingTheBindOnceReturnsTheWinnersDocument(t *testing.T) {
	s := fixedStore()

	var loads int
	load := func(context.Context) (handleRow, error) {
		loads++
		if loads == 1 {
			return unboundRow(), nil
		}
		row := unboundRow()
		row.boundTo, row.isBound, row.docID = "test-alice", true, "doc-winner"
		return row, nil
	}
	bind := func(context.Context, string) (string, error) { return "", ErrNotFound }
	materialize := func(context.Context, string, json.RawMessage) (string, error) {
		return "doc-loser", nil
	}

	got, err := s.redeem(context.Background(), "handle-1", "test-alice",
		load, bind, allow, materialize, noAudit)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if got.DocID != "doc-winner" {
		t.Errorf("DocID = %q, want the winner's document", got.DocID)
	}
	if got.FirstRedemption {
		t.Error("the losing caller reported itself as the first redemption")
	}
}

// A caller that has gone away must not buy another materialize.
func TestRedeemStopsOnACancelledContext(t *testing.T) {
	s := fixedStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	materialized := false
	materialize := func(context.Context, string, json.RawMessage) (string, error) {
		materialized = true
		return "doc", nil
	}

	_, err := s.redeem(ctx, "handle-1", "test-alice",
		func(context.Context) (handleRow, error) { return unboundRow(), nil },
		func(context.Context, string) (string, error) { return "", ErrNotFound },
		allow, materialize, noAudit)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if materialized {
		t.Error("materialized a document for a caller that had already gone")
	}
}

// A load failure is reported, not retried: it is not a lost race, and looping
// on it would turn one database problem into maxBindAttempts of them.
func TestLoadFailureIsNotRetried(t *testing.T) {
	s := fixedStore()
	boom := errors.New("connection reset")

	var loads int
	_, err := s.redeem(context.Background(), "handle-1", "test-alice",
		func(context.Context) (handleRow, error) { loads++; return handleRow{}, boom },
		func(context.Context, string) (string, error) { return "", ErrNotFound },
		allow, makeDocFn("doc"), noAudit)

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the load error", err)
	}
	if loads != 1 {
		t.Errorf("loads = %d, want 1", loads)
	}
}
