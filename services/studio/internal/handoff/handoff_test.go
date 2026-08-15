// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Against a REAL PostgreSQL instance: what these tests check is whether the
// binding statement holds under a double-click, which a fake cannot tell us.
// Set STUDIO_TEST_DATABASE_URL to run them.
package handoff

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
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

func newStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db := testDB(t)
	s := NewStore(db)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM handoff_handles WHERE creator_principal LIKE 'test-%'`)
	})
	return s, db
}

var (
	ref       = json.RawMessage(`{"run":"123"}`)
	allow     = func(context.Context, string, json.RawMessage) error { return nil }
	deny      = func(context.Context, string, json.RawMessage) error { return errors.New("denied") }
	noAudit   = Auditor(nil)
	makeDocFn = func(id string) Materializer {
		return func(context.Context, string, json.RawMessage) (string, error) { return id, nil }
	}
)

func docUUID(t *testing.T, db *sql.DB) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`SELECT gen_random_uuid()`).Scan(&id); err != nil {
		t.Fatalf("uuid: %v", err)
	}
	return id
}

func TestFirstRedemptionBindsAndMaterializes(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()

	h, err := s.Create(ctx, "test-ci", ref)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	doc := docUUID(t, db)
	got, err := s.Redeem(ctx, h.ID, "test-alice", allow, makeDocFn(doc), noAudit)
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if got.DocID != doc || !got.FirstRedemption {
		t.Fatalf("got = %+v, want doc %s and FirstRedemption", got, doc)
	}
}

// THE back-button regression guard. Strict single-use passes every security
// test and breaks the browser, so this is required rather than a concession.
func TestSecondRedemptionBySamePrincipalIsIdempotent(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()

	h, _ := s.Create(ctx, "test-ci", ref)
	doc := docUUID(t, db)

	first, err := s.Redeem(ctx, h.ID, "test-alice", allow, makeDocFn(doc), noAudit)
	if err != nil {
		t.Fatalf("first Redeem: %v", err)
	}

	// A second materializer that would return a DIFFERENT document. If it is
	// ever called, the back button silently creates a duplicate.
	other := docUUID(t, db)
	second, err := s.Redeem(ctx, h.ID, "test-alice", allow, makeDocFn(other), noAudit)
	if err != nil {
		t.Fatalf("second Redeem: %v", err)
	}
	if second.DocID != first.DocID {
		t.Fatalf("back navigation produced a different document: %s then %s", first.DocID, second.DocID)
	}
	if second.FirstRedemption {
		t.Error("second redemption reported itself as the first")
	}
}

// A different principal gets not-found, never a 403 — a 403 confirms the
// handle exists.
func TestDifferentPrincipalIsNotFound(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()

	h, _ := s.Create(ctx, "test-ci", ref)
	if _, err := s.Redeem(ctx, h.ID, "test-alice", allow, makeDocFn(docUUID(t, db)), noAudit); err != nil {
		t.Fatalf("first Redeem: %v", err)
	}

	_, err := s.Redeem(ctx, h.ID, "test-mallory", allow, makeDocFn(docUUID(t, db)), noAudit)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// Re-authorization runs on EVERY redemption, including idempotent ones. Access
// revoked at minute one must block the redemption at minute two.
func TestReauthorizationRunsOnIdempotentRedemption(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()

	h, _ := s.Create(ctx, "test-ci", ref)
	doc := docUUID(t, db)
	if _, err := s.Redeem(ctx, h.ID, "test-alice", allow, makeDocFn(doc), noAudit); err != nil {
		t.Fatalf("first Redeem: %v", err)
	}

	// Same principal, same handle — but access has since been revoked.
	if _, err := s.Redeem(ctx, h.ID, "test-alice", deny, makeDocFn(doc), noAudit); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound — binding is not authorization", err)
	}
}

func TestUnauthorizedFirstRedemptionMaterializesNothing(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	h, _ := s.Create(ctx, "test-ci", ref)

	called := false
	materialize := func(context.Context, string, json.RawMessage) (string, error) {
		called = true
		return "should-not-happen", nil
	}
	if _, err := s.Redeem(ctx, h.ID, "test-mallory", deny, materialize, noAudit); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if called {
		t.Error("materialized a document for an unauthorized principal")
	}
}

// After TTL: not-found for everyone, including the principal who bound it.
func TestExpiredHandleIsNotFound(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()

	h, _ := s.Create(ctx, "test-ci", ref)
	if _, err := db.Exec(`UPDATE handoff_handles SET expires_at = now() - interval '1 second' WHERE id = $1`, h.ID); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if _, err := s.Redeem(ctx, h.ID, "test-alice", allow, makeDocFn("d"), noAudit); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUnknownHandleIsNotFound(t *testing.T) {
	s, db := newStore(t)
	_, err := s.Redeem(context.Background(), docUUID(t, db), "test-alice", allow, makeDocFn("d"), noAudit)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// A double-click is two simultaneous redemptions. Exactly one document may be
// materialized and bound; read-then-write would produce two.
func TestConcurrentRedemptionBindsExactlyOnce(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()

	h, _ := s.Create(ctx, "test-ci", ref)

	const clicks = 20
	var wg sync.WaitGroup
	docs := make(chan string, clicks)

	for i := 0; i < clicks; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each call would materialize a DIFFERENT document if it got that
			// far, so a duplicate binding is visible rather than masked.
			r, err := s.Redeem(ctx, h.ID, "test-alice", allow, makeDocFn(docUUID(t, db)), noAudit)
			if err == nil {
				docs <- r.DocID
			}
		}()
	}
	wg.Wait()
	close(docs)

	unique := map[string]bool{}
	for d := range docs {
		unique[d] = true
	}
	if len(unique) != 1 {
		t.Fatalf("%d distinct documents from one handle; a double-click must produce one", len(unique))
	}

	var boundCount int
	if err := db.QueryRow(`SELECT count(*) FROM handoff_handles WHERE id = $1 AND bound_principal = 'test-alice'`, h.ID).Scan(&boundCount); err != nil {
		t.Fatalf("count: %v", err)
	}
	if boundCount != 1 {
		t.Fatalf("bound rows = %d, want 1", boundCount)
	}
}

// POST /v1/handoffs is otherwise an unbounded write from any bearer token.
func TestOutstandingCapIsEnforced(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	s.outstandingCap = 3

	for i := 0; i < 3; i++ {
		if _, err := s.Create(ctx, "test-spammer", ref); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	if _, err := s.Create(ctx, "test-spammer", ref); !errors.Is(err, ErrCapExceeded) {
		t.Fatalf("err = %v, want ErrCapExceeded", err)
	}
}

// The audit trail must record denied attempts, not only successes — an attempt
// on a handle bound to someone else is precisely what a trail exists for.
func TestAuditRecordsOutcomes(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()

	var mu sync.Mutex
	var events []AuditEvent
	audit := func(_ context.Context, e AuditEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}

	h, _ := s.Create(ctx, "test-ci", ref)
	doc := docUUID(t, db)

	_, _ = s.Redeem(ctx, h.ID, "test-alice", allow, makeDocFn(doc), audit)
	_, _ = s.Redeem(ctx, h.ID, "test-alice", allow, makeDocFn(doc), audit)
	_, _ = s.Redeem(ctx, h.ID, "test-mallory", allow, makeDocFn(doc), audit)

	want := []string{"redeemed_first", "redeemed_idempotent", "denied_bound_elsewhere"}
	if len(events) != len(want) {
		t.Fatalf("recorded %d events, want %d: %+v", len(events), len(want), events)
	}
	for i, outcome := range want {
		if events[i].Outcome != outcome {
			t.Errorf("event %d outcome = %q, want %q", i, events[i].Outcome, outcome)
		}
		if events[i].Creator != "test-ci" || events[i].Redeemer == "" {
			t.Errorf("event %d missing principals: %+v", i, events[i])
		}
	}
}

func TestCreateRequiresACreator(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Create(context.Background(), "", ref); err == nil {
		t.Fatal("want an error for an empty creator")
	}
}

func TestHandleExpiryIsShort(t *testing.T) {
	s, _ := newStore(t)
	h, err := s.Create(context.Background(), "test-ci", ref)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if d := time.Until(h.ExpiresAt); d > time.Hour {
		t.Errorf("TTL = %v; handles are pointers somebody is about to click, not durable shares", d)
	}
}
