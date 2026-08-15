// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package memory_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"mindclade.internal/libs/go/messaging"
	"mindclade.internal/libs/go/messaging/memory"
	"mindclade.internal/libs/go/messaging/messagingtest"
)

func TestBrokerRedeliversNackedMessage(t *testing.T) {
	broker, err := memory.NewBroker(memory.Config{Capacity: 8, MaxAttempts: 3, AckDeadline: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := broker.Subscribe("runs.created")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := broker.Publish(context.Background(), messagingtest.Message(now)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var attempts atomic.Int32
	err = subscription.Receive(ctx, func(_ context.Context, delivery messaging.Delivery) error {
		attempts.Add(1)
		if delivery.Attempt() == 1 {
			return errors.New("retry")
		}
		cancel()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts=%d want 2", attempts.Load())
	}
}

func TestBrokerEnforcesCapacity(t *testing.T) {
	broker, err := memory.NewBroker(memory.Config{Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Subscribe("runs.created"); err != nil {
		t.Fatal(err)
	}
	message := messagingtest.Message(time.Now().UTC())
	if _, err := broker.Publish(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Publish(context.Background(), message); !errors.Is(err, messaging.ErrCapacityExceeded) {
		t.Fatalf("got %v", err)
	}
}
