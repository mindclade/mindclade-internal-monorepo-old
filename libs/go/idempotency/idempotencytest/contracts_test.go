// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package idempotencytest

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcclock "mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/idempotency"
	"mindclade.internal/libs/go/identifiers"
)

func memoryFixture(t *testing.T) (*MemoryStore, *mcclock.FakeClock, idempotency.AcquireRequest) {
	t.Helper()
	now := time.Date(2026, 8, 12, 20, 0, 0, 0, time.UTC)
	clock := mcclock.NewFake(now)
	generator, err := identifiers.NewGenerator(
		identifiers.WithTimeSource(clock.Now),
		identifiers.WithEntropySource(strings.NewReader(strings.Repeat("q", 64*1024))),
	)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(WithClock(clock), WithGenerator(generator))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := idempotency.NewIdentity(
		idempotency.MustParseScope("control-plane/runs.create"),
		idempotency.MustParseKey("request-123456"),
	)
	if err != nil {
		t.Fatal(err)
	}
	request := idempotency.AcquireRequest{
		Identity:      identity,
		Fingerprint:   identifiers.SHA256String("canonical-request"),
		TTL:           time.Hour,
		LeaseDuration: time.Minute,
	}
	return store, clock, request
}

func TestMemoryStoreRenewLookupAndStaleLeaseProtection(t *testing.T) {
	t.Parallel()

	store, clock, request := memoryFixture(t)
	acquired, err := store.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	oldLease := acquired.Lease
	renewed, err := store.Renew(context.Background(), idempotency.RenewRequest{Lease: oldLease, ExtendBy: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Version != oldLease.Version+1 || !renewed.ExpiresAt.Equal(clock.Now().Add(2*time.Minute)) {
		t.Fatalf("renewed lease = %#v", renewed)
	}
	if _, err := store.Renew(context.Background(), idempotency.RenewRequest{Lease: oldLease, ExtendBy: time.Minute}); !errors.Is(err, idempotency.ErrLeaseLost) {
		t.Fatalf("stale renewal error = %v", err)
	}
	result, _ := idempotency.NewResult([]byte("done"), "text/plain", nil)
	if _, err := store.Complete(context.Background(), idempotency.CompleteRequest{Lease: oldLease, Result: result}); !errors.Is(err, idempotency.ErrLeaseLost) {
		t.Fatalf("stale completion error = %v", err)
	}
	completed, err := store.Complete(context.Background(), idempotency.CompleteRequest{Lease: renewed, Result: result})
	if err != nil {
		t.Fatal(err)
	}
	lookedUp, err := store.Lookup(context.Background(), request.Identity)
	if err != nil || lookedUp.ID() != completed.ID() || lookedUp.State() != idempotency.StateCompleted || string(lookedUp.Result().Payload()) != "done" {
		t.Fatalf("Lookup() = %#v, %v", lookedUp, err)
	}
}

func TestMemoryStoreReleaseAndExpiration(t *testing.T) {
	t.Parallel()

	store, clock, request := memoryFixture(t)
	acquired, err := store.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Release(context.Background(), idempotency.ReleaseRequest{Lease: acquired.Lease}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Lookup(context.Background(), request.Identity); !errors.Is(err, idempotency.ErrNotFound) {
		t.Fatalf("released lookup error = %v", err)
	}

	request.TTL = 2 * time.Minute
	request.LeaseDuration = time.Minute
	first, err := store.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := clock.Advance(90 * time.Second); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := store.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed.Disposition != idempotency.DispositionAcquired || !reclaimed.Lease.ExpiresAt.Equal(first.Record.ExpiresAt()) {
		t.Fatalf("reclaimed lease was not capped to retention: %#v", reclaimed.Lease)
	}
	if err := clock.Advance(30 * time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Lookup(context.Background(), request.Identity); !errors.Is(err, idempotency.ErrNotFound) {
		t.Fatalf("expired lookup error = %v", err)
	}
}

func TestMemoryStoreConcurrentAcquireHasOneOwner(t *testing.T) {
	t.Parallel()

	store, _, request := memoryFixture(t)
	const workers = 64
	var acquiredCount atomic.Int64
	var inProgressCount atomic.Int64
	var failures atomic.Int64
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(workers)
	for index := 0; index < workers; index++ {
		go func() {
			defer wait.Done()
			<-start
			acquisition, err := store.Acquire(context.Background(), request)
			if err != nil {
				failures.Add(1)
				return
			}
			switch acquisition.Disposition {
			case idempotency.DispositionAcquired:
				acquiredCount.Add(1)
			case idempotency.DispositionInProgress:
				inProgressCount.Add(1)
			default:
				failures.Add(1)
			}
		}()
	}
	close(start)
	wait.Wait()
	if failures.Load() != 0 || acquiredCount.Load() != 1 || inProgressCount.Load() != workers-1 {
		t.Fatalf("acquired=%d in_progress=%d failures=%d", acquiredCount.Load(), inProgressCount.Load(), failures.Load())
	}
}

func TestMemoryStoreContextAndConstructionValidation(t *testing.T) {
	t.Parallel()

	store, _, request := memoryFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Acquire(ctx, request); !faults.IsCode(err, faults.CodeCanceled) {
		t.Fatalf("canceled acquire error = %v", err)
	}
	if _, err := store.Acquire(nil, request); !errors.Is(err, idempotency.ErrNilContext) {
		t.Fatalf("nil context error = %v", err)
	}

	var nilClock *mcclock.FakeClock
	if _, err := NewMemoryStore(WithClock(nilClock)); err == nil {
		t.Fatal("typed nil clock accepted")
	}
	if _, err := NewMemoryStore(WithGenerator(nil)); err == nil {
		t.Fatal("nil generator accepted")
	}
	var nilStore *MemoryStore
	if _, err := nilStore.Acquire(context.Background(), request); !errors.Is(err, idempotency.ErrNilStore) {
		t.Fatalf("nil store error = %v", err)
	}
}

func TestMemoryStoreRejectsExpiredOrMismatchedReleaseLease(t *testing.T) {
	t.Parallel()

	store, clock, request := memoryFixture(t)
	acquired, err := store.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	mismatched := acquired.Lease
	mismatched.ExpiresAt = mismatched.ExpiresAt.Add(time.Second)
	if err := store.Release(context.Background(), idempotency.ReleaseRequest{Lease: mismatched}); !errors.Is(err, idempotency.ErrLeaseLost) {
		t.Fatalf("mismatched lease release error = %v", err)
	}

	if err := clock.Advance(request.LeaseDuration); err != nil {
		t.Fatal(err)
	}
	if err := store.Release(context.Background(), idempotency.ReleaseRequest{Lease: acquired.Lease}); !errors.Is(err, idempotency.ErrLeaseLost) {
		t.Fatalf("expired lease release error = %v", err)
	}

	reclaimed, err := store.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed.Disposition != idempotency.DispositionAcquired || reclaimed.Lease.Version <= acquired.Lease.Version {
		t.Fatalf("reclaimed acquisition = %#v", reclaimed)
	}
}
