// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package lease

import (
	"testing"
	"time"
)

func TestLeaseValidation(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(1, 0)
	value := Lease{Key: MustParseKey("jobs/1"), Token: token, Owner: "worker-1", Version: 1, AcquiredAt: start, ExpiresAt: start.Add(time.Minute)}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
}
