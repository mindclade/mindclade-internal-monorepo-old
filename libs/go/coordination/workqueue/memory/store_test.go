// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"mindclade.internal/libs/go/coordination/workqueue"
	"mindclade.internal/libs/go/coordination/workqueue/memory"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/requestmeta"
	"testing"
	"time"
)

func TestLeaseFencing(t *testing.T) {
	store := memory.New()
	item, err := workqueue.NewItem("events", json.RawMessage(`{"x":1}`), 0, time.Now(), 3, structZero())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Enqueue(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	claims, err := store.Claim(context.Background(), workqueue.ClaimRequest{Owner: "a", Queues: []string{"events"}, Limit: 1, LeaseDuration: time.Minute})
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim: %v %d", err, len(claims))
	}
	bad := claims[0]
	token, tokenErr := identifiers.NewUUIDv4()
	if tokenErr != nil {
		t.Fatal(tokenErr)
	}
	bad.Ownership.Token = token
	if err = store.Complete(context.Background(), bad, workqueue.Result{}, time.Now()); !errors.Is(err, workqueue.ErrLeaseLost) {
		t.Fatalf("expected lease loss: %v", err)
	}
}
func structZero() (v requestmeta.Metadata) { return }
