// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package memory

import (
	"testing"

	"go.mindclade.dev/libs/go/storage/blob"
	"go.mindclade.dev/libs/go/storage/blob/blobtest"
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
