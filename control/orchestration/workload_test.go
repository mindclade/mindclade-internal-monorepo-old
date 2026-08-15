// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import "testing"

func TestWorkloadEnvelopeTypeIsCanonicalBoundary(t *testing.T) {
	var w WorkloadEnvelope
	if w.Attempt != 0 {
		t.Fatal("zero value changed")
	}
}
