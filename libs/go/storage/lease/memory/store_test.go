// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package memory

import (
	"context"
	"testing"
	"time"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/storage/lease"
)

func TestFencing(t *testing.T) {
	fake := clock.NewFake(time.Unix(20, 0))
	store, _ := New(WithClock(fake))
	request := lease.AcquireRequest{Key: lease.MustParseKey("workers/a"), Owner: "worker-1", TTL: time.Minute}
	first, err := store.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(context.Background(), request); !faults.IsCode(err, faults.CodeConflict) {
		t.Fatalf("expected held conflict, got %v", err)
	}
	_ = fake.Advance(time.Minute)
	request.Owner = "worker-2"
	second, err := store.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Release(context.Background(), first); !faults.IsCode(err, faults.CodeConflict) {
		t.Fatalf("stale release = %v", err)
	}
	if second.Version <= first.Version {
		t.Fatal("reacquisition did not advance version")
	}
}

func TestCanceledContext(t *testing.T) {
	store, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := lease.AcquireRequest{Key: lease.MustParseKey("canceled"), Owner: "owner", TTL: time.Minute}
	if _, err := store.Acquire(ctx, request); !faults.IsCode(err, faults.CodeCanceled) {
		t.Fatalf("Acquire() = %v", err)
	}
	if _, err := store.Lookup(ctx, request.Key); !faults.IsCode(err, faults.CodeCanceled) {
		t.Fatalf("Lookup() = %v", err)
	}
}

func TestRejectsEmptyGeneratedToken(t *testing.T) {
	store, err := New(WithTokenGenerator(func() (lease.Token, error) { return lease.Token{}, nil }))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Acquire(context.Background(), lease.AcquireRequest{Key: lease.MustParseKey("empty-token"), Owner: "owner", TTL: time.Minute})
	if !faults.IsCode(err, faults.CodeInternal) || faults.ReasonOf(err) != "empty_lease_token" {
		t.Fatalf("Acquire() = %v", err)
	}
}
