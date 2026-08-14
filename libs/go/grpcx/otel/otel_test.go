// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

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
