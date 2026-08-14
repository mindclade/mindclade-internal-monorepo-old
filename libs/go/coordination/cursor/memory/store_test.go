// Copyright 2026 Mindclade. All rights reserved.
package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"mindclade.internal/libs/go/coordination/cursor"
	"mindclade.internal/libs/go/coordination/cursor/memory"
)

func TestStoreCASAndFence(t *testing.T) {
	store := memory.New()
	key, _ := cursor.NewKey("events", "registry")
	now := time.Now().UTC()
	first, err := store.Advance(context.Background(), cursor.AdvanceRequest{Key: key, Sequence: 10, Fence: 2, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || first.Sequence != 10 {
		t.Fatalf("unexpected first cursor: %+v", first)
	}
	_, err = store.Advance(context.Background(), cursor.AdvanceRequest{Key: key, ExpectedVersion: 1, Sequence: 11, Fence: 1, UpdatedAt: now.Add(time.Second)})
	if !errors.Is(err, cursor.ErrStaleFence) {
		t.Fatalf("expected stale fence, got %v", err)
	}
	second, err := store.Advance(context.Background(), cursor.AdvanceRequest{Key: key, ExpectedVersion: 1, Sequence: 11, Fence: 2, UpdatedAt: now.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if second.Version != 2 || second.Sequence != 11 {
		t.Fatalf("unexpected second cursor: %+v", second)
	}
}
