// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package outboxtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"mindclade.internal/libs/go/coordination/outbox"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/requestmeta"
)

type Factory func(*testing.T) outbox.Store

type Clock interface {
	Now() time.Time
	Advance(time.Duration) error
}

func Run(t *testing.T, factory Factory, valueClock Clock) {
	t.Helper()
	if factory == nil || valueClock == nil {
		t.Fatal("outboxtest: factory and clock are required")
	}
	t.Run("lifecycle_and_fencing", func(t *testing.T) {
		store := factory(t)
		message := Message(t, valueClock.Now(), "runs.created", []byte("payload"))
		ctx := context.Background()
		if err := store.Append(ctx, message); err != nil {
			t.Fatal(err)
		}
		if err := store.Append(ctx, message); !faults.IsCode(err, faults.CodeAlreadyExists) {
			t.Fatalf("duplicate Append() = %v", err)
		}
		claims, err := store.Claim(ctx, outbox.ClaimRequest{Owner: "dispatcher-a", Limit: 1, LeaseDuration: time.Minute})
		if err != nil || len(claims) != 1 {
			t.Fatalf("Claim() = %#v, %v", claims, err)
		}
		first := claims[0]
		renewed, err := store.Renew(ctx, first, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if renewed.Version() <= first.Version() {
			t.Fatalf("renewed version=%d first=%d", renewed.Version(), first.Version())
		}
		if err := store.MarkPublished(ctx, first, valueClock.Now()); !errors.Is(err, outbox.ErrClaimLost) {
			t.Fatalf("stale MarkPublished() = %v", err)
		}
		if err := store.MarkPublished(ctx, renewed, valueClock.Now()); err != nil {
			t.Fatal(err)
		}
		record, err := store.Lookup(ctx, message.ID().String())
		if err != nil || record.State != outbox.StatePublished || record.Attempts != 1 {
			t.Fatalf("Lookup() = %#v, %v", record, err)
		}
	})
	t.Run("expired_claim_reacquired", func(t *testing.T) {
		store := factory(t)
		message := Message(t, valueClock.Now(), "datasets.published", []byte("payload"))
		ctx := context.Background()
		if err := store.Append(ctx, message); err != nil {
			t.Fatal(err)
		}
		claims, err := store.Claim(ctx, outbox.ClaimRequest{Owner: "dispatcher-a", Limit: 1, LeaseDuration: time.Second})
		if err != nil || len(claims) != 1 {
			t.Fatalf("first claim = %#v, %v", claims, err)
		}
		if err := valueClock.Advance(2 * time.Second); err != nil {
			t.Fatal(err)
		}
		reclaimed, err := store.Claim(ctx, outbox.ClaimRequest{Owner: "dispatcher-b", Limit: 1, LeaseDuration: time.Minute})
		if err != nil || len(reclaimed) != 1 {
			t.Fatalf("reclaim = %#v, %v", reclaimed, err)
		}
		if reclaimed[0].Owner() != "dispatcher-b" || reclaimed[0].Version() <= claims[0].Version() {
			t.Fatalf("reclaimed=%#v", reclaimed[0])
		}
		if err := store.Reschedule(ctx, claims[0], valueClock.Now().Add(time.Minute), "old"); !errors.Is(err, outbox.ErrClaimLost) {
			t.Fatalf("stale Reschedule() = %v", err)
		}
	})
}

func Message(t *testing.T, now time.Time, topic string, payload []byte) outbox.Message {
	t.Helper()
	identifier, err := identifiers.NewIDAt(outbox.MessageIDKind, now)
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
	message, err := outbox.NewMessage(identifier, topic, "partition", "application/protobuf", payload, map[string]string{"schema": "v1"}, metadata, now, now)
	if err != nil {
		t.Fatal(err)
	}
	return message
}
