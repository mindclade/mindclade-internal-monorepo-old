// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package memory

import (
	"context"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/storage/cache"
)

func TestStoreCASAndExpiry(t *testing.T) {
	fake := clock.NewFake(time.Unix(10, 0))
	store, _ := New(WithClock(fake))
	key := cache.MustParseKey("run:1")
	first, err := store.Set(context.Background(), key, []byte("a"), cache.SetOptions{TTL: time.Minute, IfAbsent: true})
	if err != nil {
		t.Fatal(err)
	}
	version := first.Version
	second, err := store.Set(context.Background(), key, []byte("b"), cache.SetOptions{TTL: time.Minute, IfVersion: &version})
	if err != nil || second.Version != 2 {
		t.Fatalf("set: %#v %v", second, err)
	}
	_ = fake.Advance(time.Minute)
	if _, err := store.Get(context.Background(), key); !faults.IsCode(err, faults.CodeNotFound) {
		t.Fatalf("expected miss, got %v", err)
	}
}

func TestCanceledContext(t *testing.T) {
	store, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	key := cache.MustParseKey("canceled")
	if _, err := store.Get(ctx, key); !faults.IsCode(err, faults.CodeCanceled) {
		t.Fatalf("Get() = %v", err)
	}
	if _, err := store.Set(ctx, key, []byte("value"), cache.SetOptions{}); !faults.IsCode(err, faults.CodeCanceled) {
		t.Fatalf("Set() = %v", err)
	}
	if err := store.Delete(ctx, key, cache.DeleteOptions{}); !faults.IsCode(err, faults.CodeCanceled) {
		t.Fatalf("Delete() = %v", err)
	}
}
