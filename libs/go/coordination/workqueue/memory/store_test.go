// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/coordination/workqueue/memory"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/requestmeta"
	"testing"
	"time"
)

func TestLeaseFencing(t *testing.T) {
	store := memory.New()
	item, err := workqueue.NewItem("events", json.RawMessage(`{"x":1}`), 0, time.Now(), 3, structZero())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Enqueue(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	claims, err := store.Claim(context.Background(), workqueue.ClaimRequest{Owner: "a", Queues: []string{"events"}, Limit: 1, LeaseDuration: time.Minute})
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim: %v %d", err, len(claims))
	}
	bad := claims[0]
	token, tokenErr := identifiers.NewUUIDv4()
	if tokenErr != nil {
		t.Fatal(tokenErr)
	}
	bad.Ownership.Token = token
	if err = store.Complete(context.Background(), bad, workqueue.Result{}, time.Now()); !errors.Is(err, workqueue.ErrLeaseLost) {
		t.Fatalf("expected lease loss: %v", err)
	}
}

func TestPruneTerminalIsBoundedAndQueueScoped(t *testing.T) {
	store := memory.New()
	base := time.Now().UTC().Add(time.Second)
	oldest := completeItem(t, store, "housekeeping", base)
	old := completeItem(t, store, "housekeeping", base.Add(time.Nanosecond))
	recent := completeItem(t, store, "housekeeping", base.Add(2*time.Second))
	otherQueue := completeItem(t, store, "other", base)
	pending, err := workqueue.NewItem("housekeeping", json.RawMessage(`{"pending":true}`), 0, time.Now().UTC(), 3, structZero())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(context.Background(), pending); err != nil {
		t.Fatal(err)
	}

	request := workqueue.PruneRequest{
		Queue:           "housekeeping",
		CompletedBefore: base.Add(time.Second),
		Limit:           1,
	}
	for _, want := range []int{1, 1, 0} {
		pruned, pruneErr := store.PruneTerminal(context.Background(), request)
		if pruneErr != nil || pruned != want {
			t.Fatalf("PruneTerminal() = %d, %v, want %d, nil", pruned, pruneErr, want)
		}
	}
	for _, id := range []identifiers.ID{oldest, old} {
		if _, lookupErr := store.Lookup(context.Background(), id); !errors.Is(lookupErr, workqueue.ErrNotFound) {
			t.Fatalf("Lookup(%s) error = %v, want not found", id, lookupErr)
		}
	}
	for _, id := range []identifiers.ID{recent, otherQueue, pending.ID} {
		if _, lookupErr := store.Lookup(context.Background(), id); lookupErr != nil {
			t.Fatalf("Lookup(%s) error = %v, want retained", id, lookupErr)
		}
	}
	if _, err := store.PruneTerminal(context.Background(), workqueue.PruneRequest{
		Queue: "housekeeping", CompletedBefore: base, Limit: workqueue.MaximumPruneLimit + 1,
	}); !errors.Is(err, workqueue.ErrInvalidRequest) {
		t.Fatalf("oversized prune error = %v, want invalid request", err)
	}
}

func completeItem(t *testing.T, store *memory.Store, queue string, completedAt time.Time) identifiers.ID {
	t.Helper()
	item, err := workqueue.NewItem(queue, json.RawMessage(`{"work":true}`), 0, time.Now().UTC(), 3, structZero())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	claims, err := store.Claim(context.Background(), workqueue.ClaimRequest{
		Owner: "retention-test", Queues: []string{queue}, Limit: 1, LeaseDuration: time.Minute,
	})
	if err != nil || len(claims) != 1 {
		t.Fatalf("Claim() = %d, %v, want 1, nil", len(claims), err)
	}
	if err := store.Complete(context.Background(), claims[0], workqueue.Result{}, completedAt); err != nil {
		t.Fatal(err)
	}
	return item.ID
}

func structZero() (v requestmeta.Metadata) { return }

// TestExhaustedAttemptsCanStillReachATerminalState pins the failure mode a work
// queue exists to avoid. A worker that dies without a terminal transition
// leaves the item leased; the lease expires, the store grants a new lease, and
// that spends another attempt. Once the spent attempts pass Item.MaxAttempts
// the claim handed back must still be usable, because the only way the item
// ever leaves the queue is a Complete or a Fail made against that claim. A
// store that hands back a claim it will then reject has no dead-letter
// transition at all: the item is re-leased and its handler re-executed forever.
func TestExhaustedAttemptsCanStillReachATerminalState(t *testing.T) {
	// Rounds that must expire get a short lease; the terminal round gets a
	// generous one so a GC pause or a loaded runner cannot turn this into a
	// misleading "the item can never be dead-lettered" failure.
	const shortLease = 20 * time.Millisecond
	const terminalLease = time.Minute
	store := memory.New()
	ctx := context.Background()
	item, err := workqueue.NewItem("exhaustion", json.RawMessage(`{"poison":true}`), 0, time.Now(), 1, structZero())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Enqueue(ctx, item); err != nil {
		t.Fatal(err)
	}

	// Two lease expiries without a terminal transition: attempts now exceeds
	// the item's budget of one.
	var claim workqueue.Claim
	for round := 1; round <= 3; round++ {
		duration := shortLease
		if round == 3 {
			duration = terminalLease
		}
		claims, claimErr := store.Claim(ctx, workqueue.ClaimRequest{
			Owner: "exhaustion-test", Queues: []string{"exhaustion"}, Limit: 1, LeaseDuration: duration,
		})
		if claimErr != nil || len(claims) != 1 {
			t.Fatalf("round %d Claim() = %d claims, %v, want 1, nil", round, len(claims), claimErr)
		}
		claim = claims[0]
		if err = claim.Validate(); err != nil {
			t.Fatalf("round %d: store returned a claim it rejects: attempts=%d max_attempts=%d: %v",
				round, claim.Record.Attempts, claim.Record.Item.MaxAttempts, err)
		}
		if round < 3 {
			time.Sleep(2 * shortLease)
		}
	}
	if claim.Record.Attempts <= claim.Record.Item.MaxAttempts {
		t.Fatalf("attempts=%d did not exceed max_attempts=%d; the case under test was not reached",
			claim.Record.Attempts, claim.Record.Item.MaxAttempts)
	}
	if err = store.Fail(ctx, claim, workqueue.Failure{Reason: "attempts_exhausted", Terminal: true}, time.Now().UTC()); err != nil {
		t.Fatalf("Fail() on an over-budget claim = %v, want nil: the item can never be dead-lettered", err)
	}
	record, err := store.Lookup(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != workqueue.StateFailed {
		t.Fatalf("Lookup() state = %q, want %q", record.State, workqueue.StateFailed)
	}
}
