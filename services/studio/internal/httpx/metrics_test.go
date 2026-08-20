// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package httpx

import (
	"testing"

	"go.mindclade.dev/libs/go/observability"
)

// The collector is registered at process startup and a mismatch there aborts
// the boot, so it is worth pinning here rather than discovering in a rollout.
func TestSessionDecryptFailureMetricIsWellFormed(t *testing.T) {
	m := SessionDecryptFailureMetric()
	if m.Name != SessionDecryptFailureMetricName {
		t.Errorf("Name = %q, want %q", m.Name, SessionDecryptFailureMetricName)
	}
	if m.Kind != observability.MetricCounter {
		t.Errorf("Kind = %q, want counter", m.Kind)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// The measurement must read the live counter, not a value captured once.
func TestSessionDecryptFailureMetricTracksTheCounter(t *testing.T) {
	before := SessionDecryptFailureMetric().Value
	sessionDecryptFailures.Add(1)
	t.Cleanup(func() { sessionDecryptFailures.Add(-1) })

	if got := SessionDecryptFailureMetric().Value; got != before+1 {
		t.Errorf("value = %v, want %v", got, before+1)
	}
	if int64(SessionDecryptFailureMetric().Value) != SessionDecryptFailures() {
		t.Error("measurement and getter disagree about the same counter")
	}
}
