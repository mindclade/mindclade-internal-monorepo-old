// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package artifacts

import (
	"testing"
	"time"

	"go.mindclade.dev/libs/go/faults"
)

func testGrant(issued time.Time) AccessGrant {
	return AccessGrant{
		TenantID:         "tenant",
		ReadableDigests:  []string{"sha256:abc"},
		MaximumReadBytes: 1 << 20,
		IssuedAt:         issued,
		ExpiresAt:        issued.Add(15 * time.Minute),
	}
}

// A grant is authority over bytes, and this package has no revocation list, so
// expiry is the only thing that ever withdraws it. The type carried no expiry
// at all, which made every grant permanent by construction.
func TestGrantRequiresAValidityWindow(t *testing.T) {
	issued := time.Unix(1_700_000_000, 0).UTC()
	valid := testGrant(issued)
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}

	for name, mutate := range map[string]func(*AccessGrant){
		"missing_issue":  func(g *AccessGrant) { g.IssuedAt = time.Time{} },
		"missing_expiry": func(g *AccessGrant) { g.ExpiresAt = time.Time{} },
		"expiry_before_issue": func(g *AccessGrant) {
			g.ExpiresAt = g.IssuedAt.Add(-time.Second)
		},
		"expiry_equals_issue": func(g *AccessGrant) { g.ExpiresAt = g.IssuedAt },
	} {
		t.Run(name, func(t *testing.T) {
			grant := testGrant(issued)
			mutate(&grant)
			if err := grant.Validate(); err == nil {
				t.Fatal("a grant without a usable validity window was accepted")
			}
		})
	}

	t.Run("ttl_bound", func(t *testing.T) {
		grant := testGrant(issued)
		grant.ExpiresAt = issued.Add(MaximumGrantTTL + time.Second)
		err := grant.Validate()
		if err == nil {
			t.Fatal("a grant beyond the maximum TTL was accepted")
		}
		if reason := faults.ReasonOf(err); reason != "artifact_grant_ttl_exceeded" {
			t.Fatalf("reason=%s", reason)
		}
	})
}

// RequireActive is what a byte path calls per operation. Validating once at
// construction never turns an expiring grant into timeless authority, which is
// the contract the Rust proxy already enforces on its side.
func TestGrantStopsBeingActiveAtItsExpiry(t *testing.T) {
	issued := time.Unix(1_700_000_000, 0).UTC()
	grant := testGrant(issued)

	if !grant.Active(issued) || !grant.Active(grant.ExpiresAt.Add(-time.Nanosecond)) {
		t.Fatal("a grant inside its window was reported inactive")
	}
	if grant.Active(issued.Add(-time.Nanosecond)) {
		t.Fatal("a grant was active before it was issued")
	}
	if grant.Active(grant.ExpiresAt) {
		t.Fatal("a grant was still active at its expiry instant")
	}

	if err := grant.RequireActive(issued); err != nil {
		t.Fatal(err)
	}
	err := grant.RequireActive(grant.ExpiresAt)
	if err == nil {
		t.Fatal("an expired grant was still honoured")
	}
	if code := faults.CodeOf(err); code != faults.CodeDeadlineExceeded {
		t.Fatalf("code=%v", code)
	}
	if faults.RetryPolicyOf(err).Retryable() {
		t.Fatal("an expired grant was marked retryable; the caller must obtain a new one")
	}
}

// BuildGCPlan allocates proportionally to its input and produces one plan the
// byte plane must execute as a unit. Nothing drives it yet, so an unbounded
// batch is invisible today and immediate the moment a driver exists.
func TestBuildGCPlanRejectsAnUnboundedBatch(t *testing.T) {
	now := time.Unix(10_000, 0)
	policy := GCPolicy{MinimumAge: time.Hour}

	atBound := make([]GCArtifactState, MaximumGCPlanStates)
	for index := range atBound {
		atBound[index] = gcState(now)
	}
	if _, err := BuildGCPlan(policy, atBound, now); err != nil {
		t.Fatalf("a batch at the bound was rejected: %v", err)
	}

	// Rejecting rather than truncating: a truncated plan is still a well-formed
	// plan whose receipt validates cleanly, so the dropped objects would never
	// be collected and nothing would say so.
	plan, err := BuildGCPlan(policy, append(atBound, gcState(now)), now)
	if err == nil {
		t.Fatalf("an unbounded GC batch was accepted: %d candidates", len(plan.Candidates))
	}
	if reason := faults.ReasonOf(err); reason != "gc_plan_batch_too_large" {
		t.Fatalf("reason=%s", reason)
	}
}
