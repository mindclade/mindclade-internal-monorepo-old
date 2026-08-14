// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package coordination

import (
	"errors"
	"testing"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
)

func TestClaimFencing(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	resource, err := identifiers.NewIDAt(identifiers.MustParseKind("work"), now)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := NewClaim(resource, "worker-a", 7, now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !claim.OwnedBy("worker-a") || claim.Expired(now) || !claim.Expired(now.Add(time.Minute)) || !claim.SameEpoch(claim) {
		t.Fatalf("unexpected claim: %#v", claim)
	}
	other, err := NewClaim(resource, "worker-a", 8, now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if claim.SameEpoch(other) {
		t.Fatal("different fencing epochs matched")
	}
}

func TestFailureFromErrorIsBounded(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	err := faults.Wrap(errors.New("private"), faults.CodeUnavailable, "safe message",
		faults.WithReason("provider_unavailable"), faults.WithRetryPolicy(faults.DelayedRetry(time.Second, 3)))
	failure := FailureFromError(err, now)
	if failure.Code != faults.CodeUnavailable || failure.Reason != "provider_unavailable" || failure.Message != "safe message" || failure.RetryPolicy.After != time.Second {
		t.Fatalf("failure=%+v", failure)
	}
	if err := failure.Validate(); err != nil {
		t.Fatal(err)
	}
}
