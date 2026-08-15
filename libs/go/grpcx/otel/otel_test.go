// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package otel

import "testing"

func TestStatsHandlers(t *testing.T) {
	if NewServerStatsHandler() == nil {
		t.Fatal("nil server stats handler")
	}
	if NewClientStatsHandler() == nil {
		t.Fatal("nil client stats handler")
	}
}
