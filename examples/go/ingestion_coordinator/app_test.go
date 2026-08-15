// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package main

import (
	"context"
	"testing"
	"time"

	"mindclade.internal/libs/go/coordination/outbox"
	"mindclade.internal/libs/go/coordination/workqueue"
)

func TestIngestionCoordinatorRunsDurableWorkOutboxAndMessagingPipeline(t *testing.T) {
	application, err := BuildApplication(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !application.Kubernetes.Valid() {
		t.Fatal("local Kubernetes reference adapter was not constructed")
	}
	if err := application.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	var outboxID string
	select {
	case identifier := <-application.OutboxIDs:
		outboxID = identifier.String()
	case <-time.After(5 * time.Second):
		application.Shutdown()
		t.Fatal("worker did not append an outbox message")
	}

	select {
	case message := <-application.Received:
		if message.Topic() != snapshotTopic {
			application.Shutdown()
			t.Fatalf("topic=%s", message.Topic())
		}
	case <-time.After(5 * time.Second):
		application.Shutdown()
		t.Fatal("outbox dispatcher did not publish to the broker")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		record, lookupErr := application.WorkStore.Lookup(context.Background(), application.ItemID)
		if lookupErr == nil && record.State == workqueue.StateCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	workRecord, err := application.WorkStore.Lookup(context.Background(), application.ItemID)
	if err != nil {
		application.Shutdown()
		t.Fatal(err)
	}
	if workRecord.State != workqueue.StateCompleted {
		application.Shutdown()
		t.Fatalf("work state=%s", workRecord.State)
	}
	outboxRecord, err := application.OutboxStore.Lookup(context.Background(), outboxID)
	if err != nil {
		application.Shutdown()
		t.Fatal(err)
	}
	if outboxRecord.State != outbox.StatePublished {
		application.Shutdown()
		t.Fatalf("outbox state=%s", outboxRecord.State)
	}

	application.Shutdown()
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := application.Wait(waitCtx); err != nil {
		t.Fatal(err)
	}
}
