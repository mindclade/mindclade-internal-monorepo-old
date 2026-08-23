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

	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/coordination/workqueue/memory"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/requestmeta"
)

func TestWorkerCompletesItem(t *testing.T) {
	store := memory.New()
	requestID, err := requestmeta.NewRequestIDAt(time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	correlationID, err := requestmeta.ParseCorrelationID("controller-flow-1")
	if err != nil {
		t.Fatal(err)
	}
	causationID, err := requestmeta.ParseCausationID("scheduler-tick-1")
	if err != nil {
		t.Fatal(err)
	}
	operation, err := requestmeta.ParseOperation("controller.reconcile")
	if err != nil {
		t.Fatal(err)
	}
	wantMetadata := requestmeta.Metadata{
		RequestID: requestID, CorrelationID: correlationID, CausationID: causationID, Operation: operation,
	}
	item, err := workqueue.NewItem("controller", []byte(`{"resource":"run_1"}`), 0, time.Time{}, 3, wantMetadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	seenMetadata := make(chan requestmeta.Metadata, 1)
	worker, err := workqueue.NewWorker(
		store,
		workqueue.HandlerFunc(func(ctx context.Context, _ workqueue.Item) (workqueue.Result, error) {
			calls.Add(1)
			metadata, _ := requestmeta.FromContext(ctx)
			seenMetadata <- metadata
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
			metadata := <-seenMetadata
			if metadata.RequestID.String() != wantMetadata.RequestID.String() ||
				metadata.CorrelationID.String() != wantMetadata.CorrelationID.String() ||
				metadata.CausationID.String() != wantMetadata.CausationID.String() ||
				metadata.Operation.String() != wantMetadata.Operation.String() {
				t.Fatalf("handler metadata = %+v, want %+v", metadata, wantMetadata)
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

// TestWorkerDeadLettersItemWhoseLeaseExpiredWithoutTermination covers the
// crash path end to end. A predecessor worker leased the item and died, so the
// lease expired without a Complete or a Fail and the next lease spends an
// attempt the item's budget no longer has. The worker must still be able to
// bury the item: before this was fixed the store handed back a claim whose
// Record it then rejected, so Renew cancelled the handler, Fail was refused,
// and the item was re-leased and re-executed on every lease expiry forever.
func TestWorkerDeadLettersItemWhoseLeaseExpiredWithoutTermination(t *testing.T) {
	store := memory.New()
	item, err := workqueue.NewItem("reconcile", []byte(`{"poison":true}`), 0, time.Time{}, 1, requestmeta.Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	// Stand in for the crashed predecessor: take the item's only attempt and
	// never report a terminal transition.
	if _, err := store.Claim(context.Background(), workqueue.ClaimRequest{
		Owner: "crashed-worker", Queues: []string{"reconcile"}, Limit: 1, LeaseDuration: 20 * time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	var calls atomic.Int32
	worker, err := workqueue.NewWorker(
		store,
		workqueue.HandlerFunc(func(context.Context, workqueue.Item) (workqueue.Result, error) {
			calls.Add(1)
			return workqueue.Result{}, faults.New(faults.CodeInternal, "handler always fails", faults.WithReason("poison_payload"))
		}),
		workqueue.WorkerConfig{
			Owner:             "reconcile-1",
			Queues:            []string{"reconcile"},
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
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		record, lookupErr := store.Lookup(context.Background(), item.ID)
		if lookupErr != nil {
			t.Fatal(lookupErr)
		}
		if record.State == workqueue.StateFailed {
			if record.LastError == "" {
				t.Fatal("dead-lettered item kept no failure reason")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := store.Lookup(context.Background(), item.ID)
	t.Fatalf("item was never dead-lettered: state=%q attempts=%d handler_calls=%d",
		record.State, record.Attempts, calls.Load())
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
