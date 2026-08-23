// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outbox_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/coordination/outbox"
	"go.mindclade.dev/libs/go/coordination/outbox/memory"
	"go.mindclade.dev/libs/go/coordination/outbox/outboxtest"
	"go.mindclade.dev/libs/go/storage/lease"
)

func TestDispatcherPublishesAndCommits(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	valueClock := clock.NewFake(now)
	store, err := memory.New(memory.WithClock(valueClock))
	if err != nil {
		t.Fatal(err)
	}
	message := outboxtest.Message(t, now, "runs.created", []byte("event"))
	if err := store.Append(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	published := make(chan outbox.Message, 1)
	dispatcher, err := outbox.NewDispatcher(store, outbox.PublisherFunc(func(_ context.Context, value outbox.Message) error {
		published <- value
		return nil
	}), outbox.DispatcherConfig{Owner: "dispatcher-1", PollInterval: time.Second, ClaimDuration: time.Minute, BatchSize: 1}, outbox.WithDispatcherClock(valueClock))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- dispatcher.Run(ctx) }()
	select {
	case got := <-published:
		if !got.Equal(message) {
			t.Fatalf("published=%#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("message not published")
	}
	cancel()
	_ = valueClock.Advance(time.Second)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not stop")
	}
	record, err := store.Lookup(context.Background(), message.ID().String())
	if err != nil || record.State != outbox.StatePublished {
		t.Fatalf("record=%#v err=%v", record, err)
	}
}

// TestDispatcherAbandonsBatchOnceItHasSpentItsClaimDuration pins the bound the
// claim lease is supposed to place on one dispatcher pass. The dispatcher never
// renews a claim, so a batch whose publishes outrun ClaimDuration reaches
// messages this process no longer owns; another dispatcher has already
// reclaimed them. Publishing anyway duplicates the delivery and then fails
// MarkPublished, so the message is claimed and published again -- the outbox
// manufacturing the duplicates it exists to bound.
func TestDispatcherAbandonsBatchOnceItHasSpentItsClaimDuration(t *testing.T) {
	const claimDuration = time.Minute
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	valueClock := clock.NewFake(now)
	store := &batchClaimStore{
		claims: []outbox.Claim{
			testClaim(t, outboxtest.Message(t, now, "runs.created", []byte("first")), now, claimDuration),
			testClaim(t, outboxtest.Message(t, now, "runs.created", []byte("second")), now, claimDuration),
		},
	}
	var published atomic.Int32
	dispatcher, err := outbox.NewDispatcher(store, outbox.PublisherFunc(func(context.Context, outbox.Message) error {
		// The first publish is slow enough to burn the whole claim lease --
		// a stalled broker, a saturated link, a batch that was simply too big.
		if published.Add(1) == 1 {
			if advanceErr := valueClock.Advance(2 * claimDuration); advanceErr != nil {
				return advanceErr
			}
		}
		return nil
	}), outbox.DispatcherConfig{Owner: "dispatcher-1", PollInterval: time.Millisecond, ClaimDuration: claimDuration, BatchSize: 2},
		outbox.WithDispatcherClock(valueClock))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- dispatcher.Run(ctx) }()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if store.calls.Load() >= 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatal(runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dispatcher did not stop")
	}
	if got := published.Load(); got != 1 {
		t.Fatalf("publisher was called %d times, want 1: the second message was published under a spent claim", got)
	}
	if got := store.marked.Load(); got != 1 {
		t.Fatalf("MarkPublished was called %d times, want 1", got)
	}
	// The abandoned message must be left exactly as the store has it. Burying
	// or rescheduling it under a spent claim is the same overreach as
	// publishing it.
	if got := store.other.Load(); got != 0 {
		t.Fatalf("the dispatcher made %d other transitions, want 0", got)
	}
}

func testClaim(t *testing.T, message outbox.Message, now time.Time, duration time.Duration) outbox.Claim {
	t.Helper()
	token, err := lease.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	claim, err := outbox.NewClaim(message, token, "dispatcher-1", 2, 1, now, now.Add(duration))
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

// batchClaimStore hands the whole batch back once and records the transitions
// the dispatcher attempts.
type batchClaimStore struct {
	claims []outbox.Claim
	calls  atomic.Int32
	marked atomic.Int32
	other  atomic.Int32
}

func (store *batchClaimStore) Append(context.Context, outbox.Message) error { return nil }
func (store *batchClaimStore) Claim(context.Context, outbox.ClaimRequest) ([]outbox.Claim, error) {
	if store.calls.Add(1) == 1 {
		return store.claims, nil
	}
	return nil, nil
}
func (store *batchClaimStore) Renew(_ context.Context, claim outbox.Claim, _ time.Duration) (outbox.Claim, error) {
	store.other.Add(1)
	return claim, nil
}
func (store *batchClaimStore) MarkPublished(context.Context, outbox.Claim, time.Time) error {
	store.marked.Add(1)
	return nil
}
func (store *batchClaimStore) Reschedule(context.Context, outbox.Claim, time.Time, string) error {
	store.other.Add(1)
	return nil
}
func (store *batchClaimStore) DeadLetter(context.Context, outbox.Claim, time.Time, string) error {
	store.other.Add(1)
	return nil
}
func (store *batchClaimStore) Lookup(context.Context, string) (outbox.Record, error) {
	return outbox.Record{}, outbox.ErrNotFound
}
