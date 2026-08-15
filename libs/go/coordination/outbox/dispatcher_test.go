// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outbox_test

import (
	"context"
	"testing"
	"time"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/coordination/outbox"
	"mindclade.internal/libs/go/coordination/outbox/memory"
	"mindclade.internal/libs/go/coordination/outbox/outboxtest"
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
