// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package memory_test

import (
	"testing"

	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/coordination/workqueue/memory"
	"go.mindclade.dev/libs/go/coordination/workqueue/workqueuetest"
)

// The shared suite is what makes this adapter and the PostgreSQL one answerable
// to one contract. Until something called it, it asserted nothing about either,
// and its waiver in libs/go/UNCONSUMED.toml said as much: clearing it was "a
// change in libs/go/coordination, not a wait".
func TestConformance(t *testing.T) {
	// Checked here rather than left to the suite: a nil store surfaces inside
	// the suite as a panic on whichever case happens to run first, which reads
	// as a broken contract rather than a broken constructor.
	if store := memory.New(); store == nil {
		t.Fatal("the memory work-queue constructor returned a nil store")
	}
	workqueuetest.Conformance(t, func() workqueue.Store {
		return memory.New()
	})
}
