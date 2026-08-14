// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package memory

import (
	"testing"

	"mindclade.internal/libs/go/storage/cache"
	"mindclade.internal/libs/go/storage/cache/cachetest"
)

func TestConformance(t *testing.T) {
	cachetest.Run(t, func(t testing.TB) cache.Store {
		t.Helper()
		store, err := New()
		if err != nil {
			t.Fatal(err)
		}
		return store
	})
}
