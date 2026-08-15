// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package workqueue_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"mindclade.internal/libs/go/coordination/workqueue"
	"mindclade.internal/libs/go/coordination/workqueue/memory"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/requestmeta"
)

func TestWorkerCompletesItem(t *testing.T) {
	store := memory.New()
	item, err := workqueue.NewItem("controller", []byte(`{"resource":"run_1"}`), 0, time.Time{}, 3, requestmeta.Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	worker, err := workqueue.NewWorker(
		store,
		workqueue.HandlerFunc(func(context.Context, workqueue.Item) (workqueue.Result, error) {
			calls.Add(1)
			return workqueue.Result{ContentType: "application/json", Payload: []byte(`{"ok":true}`)}, nil
		}),
		workqueue.WorkerConfig{
			Owner:             "controller-1",
			Queues:            []string{"controller"},
			PollInterval:      time.Millisecond,
			LeaseDuration:     time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			BatchSize:         1,
			Concurrency:       1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		record, lookupErr := store.Lookup(context.Background(), item.ID)
		if lookupErr == nil && record.State == workqueue.StateCompleted {
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 {
				t.Fatalf("calls=%d", calls.Load())
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("item was not completed")
}

func TestWorkerRenewsLeaseDuringLongHandler(t *testing.T) {
	base := memory.New()
	store := &renewCountingStore{Store: base}
	item, err := workqueue.NewItem("ingestion", []byte(`{"source":"pdb"}`), 0, time.Time{}, 3, requestmeta.Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	worker, err := workqueue.NewWorker(
		store,
		workqueue.HandlerFunc(func(ctx context.Context, _ workqueue.Item) (workqueue.Result, error) {
			deadline := time.NewTimer(time.Second)
			defer deadline.Stop()
			for store.renewals.Load() < 2 {
				select {
				case <-ctx.Done():
					return workqueue.Result{}, ctx.Err()
				case <-deadline.C:
					return workqueue.Result{}, faults.New(faults.CodeDeadlineExceeded, "renewal was not observed", faults.WithReason("renewal_missing"))
				case <-time.After(time.Millisecond):
				}
			}
			return workqueue.Result{}, nil
		}),
		workqueue.WorkerConfig{
			Owner:             "ingestion-1",
			Queues:            []string{"ingestion"},
			PollInterval:      time.Millisecond,
			LeaseDuration:     80 * time.Millisecond,
			HeartbeatInterval: 10 * time.Millisecond,
			BatchSize:         1,
			Concurrency:       1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		record, lookupErr := store.Lookup(context.Background(), item.ID)
		if lookupErr == nil && record.State == workqueue.StateCompleted {
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if store.renewals.Load() < 2 {
				t.Fatalf("renewals=%d", store.renewals.Load())
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("long-running item did not complete")
}

type renewCountingStore struct {
	workqueue.Store
	renewals atomic.Int32
}

func (store *renewCountingStore) Renew(ctx context.Context, claim workqueue.Claim, duration time.Duration) (workqueue.Claim, error) {
	renewed, err := store.Store.Renew(ctx, claim, duration)
	if err == nil {
		store.renewals.Add(1)
	}
	return renewed, err
}
