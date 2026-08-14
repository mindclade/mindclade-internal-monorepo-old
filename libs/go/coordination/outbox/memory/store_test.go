// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package memory

import (
	"testing"
	"time"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/coordination/outbox"
	"mindclade.internal/libs/go/coordination/outbox/outboxtest"
)

func TestConformance(t *testing.T) {
	valueClock := clock.NewFake(time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC))
	outboxtest.Run(t, func(t *testing.T) outbox.Store {
		t.Helper()
		store, err := New(WithClock(valueClock))
		if err != nil {
			t.Fatal(err)
		}
		return store
	}, valueClock)
}
