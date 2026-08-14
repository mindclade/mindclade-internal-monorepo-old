// Copyright 2026 Mindclade. All rights reserved.
package workqueuetest

import (
	"context"
	"encoding/json"
	"mindclade.internal/libs/go/coordination/workqueue"
	"mindclade.internal/libs/go/requestmeta"
	"testing"
	"time"
)

type Factory func() workqueue.Store

func Conformance(t *testing.T, factory Factory) {
	t.Helper()
	store := factory()
	item, err := workqueue.NewItem("conformance", json.RawMessage(`{"v":1}`), 1, time.Now(), 2, requestmeta.Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Enqueue(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	claims, err := store.Claim(context.Background(), workqueue.ClaimRequest{Owner: "test", Queues: []string{"conformance"}, Limit: 1, LeaseDuration: time.Minute})
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim %v %d", err, len(claims))
	}
	if err = store.Complete(context.Background(), claims[0], workqueue.Result{}, time.Now()); err != nil {
		t.Fatal(err)
	}
}
