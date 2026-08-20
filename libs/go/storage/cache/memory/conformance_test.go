// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package memory

import (
	"testing"

	"go.mindclade.dev/libs/go/storage/cache"
	"go.mindclade.dev/libs/go/storage/cache/cachetest"
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
