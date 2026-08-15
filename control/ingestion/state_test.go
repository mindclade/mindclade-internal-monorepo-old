// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package ingestion

import "testing"

func TestStateMachine(t *testing.T) {
	if !CanTransition(StatePending, StateRunning) || !CanTransition(StateRunning, StateCompleted) || CanTransition(StateCompleted, StateRunning) {
		t.Fatal("invalid ingestion state transitions")
	}
}
