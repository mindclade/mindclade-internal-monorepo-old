// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package outbox_test

import (
	"context"
	"testing"
	"time"

	"mindclade.internal/libs/go/clock"
	canonicalmemory "mindclade.internal/libs/go/coordination/outbox/memory"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/requestmeta"
	storageoutbox "mindclade.internal/libs/go/storage/outbox"
)

func TestStorageFacadeUsesCanonicalImplementation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	fakeClock := clock.NewFake(now)
	store, err := canonicalmemory.New(canonicalmemory.WithClock(fakeClock))
	if err != nil {
		t.Fatal(err)
	}
	var repository storageoutbox.Repository = store
	id, err := identifiers.NewIDAt(storageoutbox.EnvelopeIDKind, now)
	if err != nil {
		t.Fatal(err)
	}
	requestID, err := requestmeta.NewRequestIDAt(now)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := requestmeta.New(requestID)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := storageoutbox.NewEnvelope(id, "runs.created", "run-1", "application/protobuf", []byte("payload"), nil, metadata, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Append(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
	claims, err := repository.Claim(context.Background(), storageoutbox.ClaimRequest{Owner: "dispatcher", Limit: 1, LeaseDuration: time.Minute})
	if err != nil || len(claims) != 1 {
		t.Fatalf("Claim() = %#v, %v", claims, err)
	}
	if err := repository.MarkPublished(context.Background(), claims[0], now); err != nil {
		t.Fatal(err)
	}
	record, err := repository.Lookup(context.Background(), envelope.ID().String())
	if err != nil {
		t.Fatal(err)
	}
	if record.State != storageoutbox.StatusPublished {
		t.Fatalf("state = %q", record.State)
	}
}
