// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package cursortest

import (
	"context"
	"errors"
	"testing"
	"time"

	"mindclade.internal/libs/go/coordination/cursor"
)

type Factory func() cursor.Store

func Conformance(t *testing.T, factory Factory) {
	t.Helper()
	store := factory()
	key, _ := cursor.NewKey("conformance", "cursor")
	now := time.Now().UTC()
	created, err := store.Advance(context.Background(), cursor.AdvanceRequest{Key: key, Sequence: 1, Fence: 1, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != created.Version {
		t.Fatal("load mismatch")
	}
	_, err = store.Advance(context.Background(), cursor.AdvanceRequest{Key: key, ExpectedVersion: 999, Sequence: 2, Fence: 1, UpdatedAt: now.Add(time.Second)})
	if !errors.Is(err, cursor.ErrConflict) {
		t.Fatalf("expected conflict: %v", err)
	}
	if err := store.Delete(context.Background(), key, created.Version); err != nil {
		t.Fatal(err)
	}
	_, err = store.Load(context.Background(), key)
	if !errors.Is(err, cursor.ErrNotFound) {
		t.Fatalf("expected not found: %v", err)
	}
}
