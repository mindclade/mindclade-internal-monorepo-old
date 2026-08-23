// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package memory_test

import (
	"testing"

	"go.mindclade.dev/libs/go/coordination/cursor"
	"go.mindclade.dev/libs/go/coordination/cursor/cursortest"
	"go.mindclade.dev/libs/go/coordination/cursor/memory"
)

// See the note on the work-queue memory store: compare-and-advance is a contract
// shared with the PostgreSQL adapter, so the suite has to run against at least
// one implementation to mean anything.
func TestConformance(t *testing.T) {
	if store := memory.New(); store == nil {
		t.Fatal("the memory cursor constructor returned a nil store")
	}
	cursortest.Conformance(t, func() cursor.Store {
		return memory.New()
	})
}
