// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package idempotencytest

import (
	"context"
	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/identifiers"
	"strings"
	"testing"
	"time"
)

func TestMemoryStoreLeaseReclaimAndCompletion(t *testing.T) {
	now := time.Date(2026, 8, 12, 19, 0, 0, 0, time.UTC)
	clock := mcclock.NewFake(now)
	generator, err := identifiers.NewGenerator(identifiers.WithTimeSource(clock.Now), identifiers.WithEntropySource(strings.NewReader(strings.Repeat("m", 8192))))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(WithClock(clock), WithGenerator(generator))
	if err != nil {
		t.Fatal(err)
	}
	identity, _ := idempotency.NewIdentity(idempotency.MustParseScope("control/runs.create"), idempotency.MustParseKey("request-123456"))
	request := idempotency.AcquireRequest{Identity: identity, Fingerprint: identifiers.SHA256String("body"), TTL: time.Hour, LeaseDuration: time.Minute}
	first, err := store.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Acquire(context.Background(), request)
	if err != nil || second.Disposition != idempotency.DispositionInProgress {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if err := clock.Advance(time.Minute); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := store.Acquire(context.Background(), request)
	if err != nil || reclaimed.Disposition != idempotency.DispositionAcquired || reclaimed.Lease.Version == first.Lease.Version {
		t.Fatalf("reclaimed=%+v err=%v", reclaimed, err)
	}
	result, _ := idempotency.NewResult([]byte("done"), "text/plain", nil)
	completed, err := store.Complete(context.Background(), idempotency.CompleteRequest{Lease: reclaimed.Lease, Result: result})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := store.Acquire(context.Background(), request)
	if err != nil || replay.Disposition != idempotency.DispositionReplay || completed.State() != idempotency.StateCompleted {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}
