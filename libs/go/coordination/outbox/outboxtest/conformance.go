// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outboxtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/coordination/outbox"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/requestmeta"
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
	// Every mutator, not just the two above. MarkPublished and Reschedule were already fenced
	// here; DeadLetter and Renew were not, and a fence with a hole in it is not a fence -- a
	// superseded dispatcher that cannot publish could still have buried the message, or held
	// its own claim alive indefinitely against the owner that replaced it.
	t.Run("every_mutator_rejects_a_superseded_claim", func(t *testing.T) {
		store := factory(t)
		message := Message(t, valueClock.Now(), "models.promoted", []byte("payload"))
		ctx := context.Background()
		if err := store.Append(ctx, message); err != nil {
			t.Fatal(err)
		}
		claims, err := store.Claim(ctx, outbox.ClaimRequest{Owner: "dispatcher-a", Limit: 1, LeaseDuration: time.Second})
		if err != nil || len(claims) != 1 {
			t.Fatalf("first claim = %#v, %v", claims, err)
		}
		stale := claims[0]
		if err := valueClock.Advance(2 * time.Second); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Claim(ctx, outbox.ClaimRequest{Owner: "dispatcher-b", Limit: 1, LeaseDuration: time.Minute}); err != nil {
			t.Fatal(err)
		}
		if err := store.DeadLetter(ctx, stale, valueClock.Now(), "stale owner"); !errors.Is(err, outbox.ErrClaimLost) {
			t.Fatalf("stale DeadLetter() = %v, want ErrClaimLost", err)
		}
		if _, err := store.Renew(ctx, stale, time.Minute); !errors.Is(err, outbox.ErrClaimLost) {
			t.Fatalf("stale Renew() = %v, want ErrClaimLost", err)
		}
		if err := store.MarkPublished(ctx, stale, valueClock.Now()); !errors.Is(err, outbox.ErrClaimLost) {
			t.Fatalf("stale MarkPublished() = %v, want ErrClaimLost", err)
		}
		record, err := store.Lookup(ctx, message.ID().String())
		if err != nil {
			t.Fatal(err)
		}
		if record.State != outbox.StateClaimed {
			t.Fatalf("a rejected mutator moved the record: state=%q", record.State)
		}
	})
	// The crash the outbox exists to survive: the broker accepted the message and the process
	// died before MarkPublished recorded it. Nothing durable distinguishes that from a publish
	// that never happened, so the record must come back and be redelivered -- at-least-once,
	// which is why consumers dedupe on the message ID.
	t.Run("crash_before_mark_published_redelivers", func(t *testing.T) {
		store := factory(t)
		message := Message(t, valueClock.Now(), "artifacts.committed", []byte("payload"))
		ctx := context.Background()
		if err := store.Append(ctx, message); err != nil {
			t.Fatal(err)
		}
		first, err := store.Claim(ctx, outbox.ClaimRequest{Owner: "dispatcher-a", Limit: 1, LeaseDuration: time.Second})
		if err != nil || len(first) != 1 {
			t.Fatalf("first claim = %#v, %v", first, err)
		}
		// Publish succeeds here. MarkPublished is never called: the process is gone.
		if err := valueClock.Advance(2 * time.Second); err != nil {
			t.Fatal(err)
		}
		second, err := store.Claim(ctx, outbox.ClaimRequest{Owner: "dispatcher-b", Limit: 1, LeaseDuration: time.Minute})
		if err != nil || len(second) != 1 {
			t.Fatalf("redelivery claim = %#v, %v", second, err)
		}
		if second[0].Attempts() <= first[0].Attempts() {
			t.Fatalf("redelivery did not count an attempt: first=%d second=%d",
				first[0].Attempts(), second[0].Attempts())
		}
		if err := store.MarkPublished(ctx, second[0], valueClock.Now()); err != nil {
			t.Fatal(err)
		}
		record, err := store.Lookup(ctx, message.ID().String())
		if err != nil || record.State != outbox.StatePublished {
			t.Fatalf("Lookup() = %#v, %v", record, err)
		}
		if record.Attempts < 2 {
			t.Fatalf("attempts=%d, want the redelivery counted", record.Attempts)
		}
	})
	// Dead-lettering is terminal. A record that has been buried must not be reachable by a
	// later Claim, or a poison message rotates back into the batch forever.
	t.Run("dead_letter_is_terminal", func(t *testing.T) {
		store := factory(t)
		message := Message(t, valueClock.Now(), "webhooks.failed", []byte("payload"))
		ctx := context.Background()
		if err := store.Append(ctx, message); err != nil {
			t.Fatal(err)
		}
		claims, err := store.Claim(ctx, outbox.ClaimRequest{Owner: "dispatcher-a", Limit: 1, LeaseDuration: time.Minute})
		if err != nil || len(claims) != 1 {
			t.Fatalf("Claim() = %#v, %v", claims, err)
		}
		if err := store.DeadLetter(ctx, claims[0], valueClock.Now(), "exhausted"); err != nil {
			t.Fatal(err)
		}
		record, err := store.Lookup(ctx, message.ID().String())
		if err != nil || record.State != outbox.StateDeadLetter {
			t.Fatalf("Lookup() = %#v, %v", record, err)
		}
		if record.LastError == "" {
			t.Fatal("dead-lettered record kept no reason")
		}
		if err := valueClock.Advance(time.Hour); err != nil {
			t.Fatal(err)
		}
		again, err := store.Claim(ctx, outbox.ClaimRequest{Owner: "dispatcher-b", Limit: 10, LeaseDuration: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		for _, claim := range again {
			if claim.Message().ID() == message.ID() {
				t.Fatal("a dead-lettered message was claimed again")
			}
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
