// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package memory

import (
	"testing"

	"mindclade.internal/libs/go/storage/lease"
	"mindclade.internal/libs/go/storage/lease/leasetest"
)

func TestConformance(t *testing.T) {
	leasetest.Run(t, func(t testing.TB) lease.Store {
		t.Helper()
		store, err := New()
		if err != nil {
			t.Fatal(err)
		}
		return store
	})
}
