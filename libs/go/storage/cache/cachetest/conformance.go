// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package cachetest

import (
	"context"
	"testing"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/storage/cache"
)

type Factory func(testing.TB) cache.Store

func Run(t *testing.T, factory Factory) {
	t.Helper()
	store := factory(t)
	ctx := context.Background()
	key := cache.MustParseKey("conformance:key")
	first, err := store.Set(ctx, key, []byte("one"), cache.SetOptions{IfAbsent: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.Version == 0 {
		t.Fatal("zero version")
	}
	if _, err := store.Set(ctx, key, []byte("duplicate"), cache.SetOptions{IfAbsent: true}); !faults.IsCode(err, faults.CodeAlreadyExists) {
		t.Fatalf("IfAbsent = %v", err)
	}
	read, err := store.Get(ctx, key)
	if err != nil || string(read.Value) != "one" {
		t.Fatalf("Get = %#v, %v", read, err)
	}
	expected := first.Version
	second, err := store.Set(ctx, key, []byte("two"), cache.SetOptions{IfVersion: &expected})
	if err != nil {
		t.Fatal(err)
	}
	if second.Version <= first.Version {
		t.Fatal("version did not advance")
	}
	if err := store.Delete(ctx, key, cache.DeleteOptions{IfVersion: &expected}); !faults.IsCode(err, faults.CodeConflict) {
		t.Fatalf("stale Delete = %v", err)
	}
	expected = second.Version
	if err := store.Delete(ctx, key, cache.DeleteOptions{IfVersion: &expected}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, key); !faults.IsCode(err, faults.CodeNotFound) {
		t.Fatalf("Get after delete = %v", err)
	}
}
