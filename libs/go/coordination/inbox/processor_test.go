// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package inbox_test

import (
	"context"
	"mindclade.internal/libs/go/coordination/inbox"
	"mindclade.internal/libs/go/idempotency"
	"mindclade.internal/libs/go/idempotency/idempotencytest"
	"mindclade.internal/libs/go/identifiers"
	"testing"
)

func TestProcessAndReplay(t *testing.T) {
	store, err := idempotencytest.NewMemoryStore()
	if err != nil {
		t.Fatal(err)
	}
	runner := inbox.RunnerFunc(func(ctx context.Context, work func(context.Context) error) error { return work(ctx) })
	processor, err := inbox.New(runner, store)
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := idempotency.ParseScope("events.registry")
	key, _ := idempotency.ParseKey("event-00000001")
	identity, _ := idempotency.NewIdentity(scope, key)
	message := inbox.Message{Identity: identity, Fingerprint: identifiers.SHA256([]byte("payload"))}
	calls := 0
	handler := func(context.Context) (idempotency.Result, error) { calls++; return idempotency.EmptyResult() }
	first, err := processor.Process(context.Background(), message, handler)
	if err != nil {
		t.Fatal(err)
	}
	second, err := processor.Process(context.Background(), message, handler)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Processed || !second.Duplicate || calls != 1 {
		t.Fatalf("first=%+v second=%+v calls=%d", first, second, calls)
	}
}
