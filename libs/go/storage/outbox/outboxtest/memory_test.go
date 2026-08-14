// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package outboxtest

import (
	"testing"
	"time"

	"mindclade.internal/libs/go/clock"
	storageoutbox "mindclade.internal/libs/go/storage/outbox"
)

func TestMemoryStoreConformance(t *testing.T) {
	start := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	fakeClock := clock.NewFake(start)
	Run(t, func(t *testing.T) storageoutbox.Repository {
		t.Helper()
		store, err := NewMemory(WithClock(fakeClock))
		if err != nil {
			t.Fatal(err)
		}
		return store
	}, fakeClock)
}
