// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package memory

import (
	"testing"

	"mindclade.internal/libs/go/storage/blob"
	"mindclade.internal/libs/go/storage/blob/blobtest"
)

func TestConformance(t *testing.T) {
	blobtest.Run(t, func(t testing.TB) blob.Store {
		t.Helper()
		store, err := New()
		if err != nil {
			t.Fatal(err)
		}
		return store
	})
}
