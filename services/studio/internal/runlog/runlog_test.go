// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// These tests run against a REAL PostgreSQL instance, because what they check
// is not Go logic — it is whether three SQL statements hold under concurrency.
// A fake would assert that the test's own model of the database is consistent
// with itself, which is exactly the property that does not matter here.
//
// Set STUDIO_TEST_DATABASE_URL to run them; they skip otherwise, so a laptop
// without a database still gets a green build.
package runlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("STUDIO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("STUDIO_TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newRun inserts a run and returns its id, cleaning up afterwards.
func newRun(t *testing.T, db *sql.DB, submitter, key string) string {
	t.Helper()
	var id string
	err := db.QueryRow(`
		INSERT INTO runs (id, submitter, idempotency_key, request_digest)
		VALUES (gen_random_uuid(), $1, $2, 'digest')
		RETURNING id`, submitter, key).Scan(&id)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM run_events WHERE run_id = $1`, id)
		_, _ = db.Exec(`DELETE FROM run_results WHERE run_id = $1`, id)
		_, _ = db.Exec(`DELETE FROM runs WHERE id = $1`, id)
	})
	return id
}

func TestAppendAllocatesDenseSequence(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	ctx := context.Background()
	runID := newRun(t, db, "alice", "dense-"+time.Now().Format(time.RFC3339Nano))

	for i := int64(1); i <= 10; i++ {
		seq, err := s.Append(ctx, runID, "token", json.RawMessage(`{"t":"x"}`))
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
		if seq != i {
			t.Fatalf("seq = %d, want %d", seq, i)
		}
	}
}

// Density under concurrency is the property Last-Event-ID depends on: the
// client cannot distinguish "no event yet" from "event lost", so a gap is
// indistinguishable from data loss.
//
// One writer per run is what makes this safe; this test asserts the statement
// is a backstop rather than a race, by violating that assumption deliberately.
func TestConcurrentAppendsLeaveNoGaps(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	ctx := context.Background()
	runID := newRun(t, db, "alice", "concurrent-"+time.Now().Format(time.RFC3339Nano))

	const writers = 20
	var wg sync.WaitGroup
	seqs := make(chan int64, writers)

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			seq, err := s.Append(ctx, runID, "token", json.RawMessage(`{"t":"x"}`))
			if err != nil {
				return // a lost race is acceptable; a DUPLICATE seq is not
			}
			seqs <- seq
		}()
	}
	wg.Wait()
	close(seqs)

	seen := map[int64]bool{}
	var max int64
	for seq := range seqs {
		if seen[seq] {
			t.Fatalf("seq %d allocated twice — resume would skip or repeat output", seq)
		}
		seen[seq] = true
		if seq > max {
			max = seq
		}
	}
	for i := int64(1); i <= max; i++ {
		if !seen[i] {
			t.Fatalf("gap at seq %d — the client cannot tell this from a lost event", i)
		}
	}
}

func TestReadFromReturnsEventsAfterTheCursor(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	ctx := context.Background()
	runID := newRun(t, db, "alice", "read-"+time.Now().Format(time.RFC3339Nano))

	for i := 0; i < 5; i++ {
		if _, err := s.Append(ctx, runID, "token", json.RawMessage(`{"t":"x"}`)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	started := time.Now().Add(-time.Hour)
	events, err := s.ReadFrom(ctx, runID, 2, started, 100)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("len = %d, want 3", len(events))
	}
	if events[0].Seq != 3 {
		t.Errorf("first seq = %d, want 3 — resume must not repeat the cursor", events[0].Seq)
	}
	for i := 1; i < len(events); i++ {
		if events[i].Seq <= events[i-1].Seq {
			t.Fatalf("events out of order: %d after %d", events[i].Seq, events[i-1].Seq)
		}
	}
}

// A transcript row is a REFERENCE to a result, not the result. Without this the
// events table becomes the largest object in the database.
func TestOversizedPayloadIsRejected(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	ctx := context.Background()
	runID := newRun(t, db, "alice", "big-"+time.Now().Format(time.RFC3339Nano))

	huge := json.RawMessage(`{"data":"` + strings.Repeat("x", MaxPayloadBytes) + `"}`)
	if _, err := s.Append(ctx, runID, "blob", huge); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("err = %v, want ErrPayloadTooLarge", err)
	}
}

// Twenty concurrent submissions under one key must produce exactly one run.
// Run CONCURRENTLY, not sequentially: sequentially this passes against a
// read-then-insert implementation, which is the implementation the test exists
// to reject.
func TestConcurrentSubmitIsIdempotent(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	ctx := context.Background()

	key := "idem-" + time.Now().Format(time.RFC3339Nano)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM runs WHERE idempotency_key = $1`, key) })

	const attempts = 20
	var wg sync.WaitGroup
	ids := make(chan string, attempts)
	created := make(chan bool, attempts)

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, isNew, err := s.Submit(ctx, newUUID(t, db), "alice", key, "digest-a")
			if err != nil {
				return
			}
			ids <- id
			created <- isNew
		}()
	}
	wg.Wait()
	close(ids)
	close(created)

	unique := map[string]bool{}
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != 1 {
		t.Fatalf("%d distinct run ids returned; every caller must get the same one", len(unique))
	}

	creations := 0
	for c := range created {
		if c {
			creations++
		}
	}
	if creations != 1 {
		t.Errorf("created=true returned %d times, want 1", creations)
	}

	var rows int
	if err := db.QueryRow(`SELECT count(*) FROM runs WHERE idempotency_key = $1`, key).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Fatalf("%d rows in runs; a duplicate GPU job is a real cost", rows)
	}
}

// The same key with a different body is a 409, not a silent reuse.
func TestSubmitRejectsDifferentBodyForSameKey(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	ctx := context.Background()

	key := "mismatch-" + time.Now().Format(time.RFC3339Nano)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM runs WHERE idempotency_key = $1`, key) })

	if _, _, err := s.Submit(ctx, newUUID(t, db), "alice", key, "digest-a"); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if _, _, err := s.Submit(ctx, newUUID(t, db), "alice", key, "digest-b"); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
}

// Cancellation is durable and reports whether THIS call set it, so a second
// cancel is a 409 rather than a silent success.
func TestRequestCancelIsIdempotentlyReported(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	ctx := context.Background()
	runID := newRun(t, db, "alice", "cancel-"+time.Now().Format(time.RFC3339Nano))

	first, err := s.RequestCancel(ctx, runID)
	if err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	if !first {
		t.Fatal("first cancel should report that it set the flag")
	}

	second, err := s.RequestCancel(ctx, runID)
	if err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	if second {
		t.Fatal("second cancel should report that the flag was already set")
	}

	meta, err := s.Run(ctx, runID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if meta.CancelledAt == nil {
		t.Fatal("cancellation did not persist — the executor would never see it")
	}
}

func TestRunNotFound(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	if _, err := s.Run(context.Background(), newUUID(t, db)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func newUUID(t *testing.T, db *sql.DB) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`SELECT gen_random_uuid()`).Scan(&id); err != nil {
		t.Fatalf("uuid: %v", err)
	}
	return id
}
