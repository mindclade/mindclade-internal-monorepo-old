// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package artifacts

import (
	"mindclade.internal/libs/go/identifiers"
	"testing"
	"time"
)

func gcRef() Ref {
	return Ref{Digest: identifiers.SHA256([]byte("artifact")), SizeBytes: 8, MediaType: "application/octet-stream", LogicalKind: "dataset_shard", SchemaVersion: 1}
}
func gcState(now time.Time) GCArtifactState {
	return GCArtifactState{Ref: gcRef(), CreatedAt: now.Add(-2 * time.Hour), ObjectPath: "sha256/aa/object", ObservedObjectVersion: "rv1:7:sha256:1e5b7d7e572b9d82ea29db72b05d3f5b80f6c8c6ce5d28da3177748fe2d98a6f"}
}
func TestGCRequiresUnreachableUnleasedUnpinnedExpiredRetention(t *testing.T) {
	now := time.Unix(1000, 0)
	state := gcState(now)
	policy := GCPolicy{MinimumAge: time.Hour}
	if d := policy.Evaluate(state, now); d.Candidate == nil {
		t.Fatalf("expected candidate: %#v", d)
	}
	state.Reachability.DurableReferences = 1
	if d := policy.Evaluate(state, now); d.Candidate != nil {
		t.Fatal("reachable artifact must not be collected")
	}
	state.Reachability = ArtifactReachability{}
	state.Leases = []ArtifactLease{{LeaseID: "lease", ExpiresAt: now.Add(time.Minute)}}
	if d := policy.Evaluate(state, now); d.Candidate != nil {
		t.Fatal("leased artifact must not be collected")
	}
}
func TestGCRejectsUnboundLocation(t *testing.T) {
	now := time.Unix(1000, 0)
	state := gcState(now)
	state.ObjectPath = "../escape"
	if d := (GCPolicy{MinimumAge: time.Hour}).Evaluate(state, now); d.Candidate != nil {
		t.Fatal("unsafe path must not be collected")
	}
}
func TestGCPlanAndReceiptAreDeterministic(t *testing.T) {
	now := time.Unix(1000, 0)
	p := GCPolicy{MinimumAge: time.Hour}
	a := gcState(now)
	one, err := BuildGCPlan(p, []GCArtifactState{a}, now)
	if err != nil {
		t.Fatal(err)
	}
	two, err := BuildGCPlan(p, []GCArtifactState{a}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !one.PlanID.Equal(two.PlanID) {
		t.Fatal("plan digest must be deterministic")
	}
	receipt := GCReceipt{PlanID: one.PlanID, CompletedAt: now, Results: []GCSweepResult{{Digest: a.Ref.Digest, ObjectPath: a.ObjectPath, Outcome: GCSweepDeleted}}}
	if err := ValidateGCReceipt(one, receipt); err != nil {
		t.Fatal(err)
	}
}

func TestGCBlocksReleaseAuditPinsHoldsAndRetention(t *testing.T) {
	now := time.Unix(10_000, 0)
	policy := GCPolicy{MinimumAge: time.Hour}

	cases := []struct {
		name   string
		mutate func(*GCArtifactState)
		reason GCBlockReason
	}{
		{"release evidence", func(s *GCArtifactState) { s.Reachability.ReleaseEvidenceReferences = 1 }, GCReachable},
		{"audit reference", func(s *GCArtifactState) { s.Reachability.AuditReferences = 1 }, GCReachable},
		{"active lease", func(s *GCArtifactState) {
			s.Leases = []ArtifactLease{{LeaseID: "lease", ExpiresAt: now.Add(time.Minute)}}
		}, GCActiveLease},
		{"administrative pin", func(s *GCArtifactState) { s.Pins = []ArtifactPin{{PinID: "pin", Reason: "incident"}} }, GCAdministrativePin},
		{"retention hold", func(s *GCArtifactState) { s.Holds = []RetentionHold{{HoldID: "hold", Reason: "legal"}} }, GCRetentionHold},
		{"retention window", func(s *GCArtifactState) { s.CreatedAt = now.Add(-time.Minute) }, GCRetentionWindow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := gcState(now)
			tc.mutate(&state)
			decision := policy.Evaluate(state, now)
			if decision.Candidate != nil {
				t.Fatal("blocked artifact became GC candidate")
			}
			found := false
			for _, reason := range decision.BlockedBy {
				if reason == tc.reason {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("missing block reason %q in %#v", tc.reason, decision.BlockedBy)
			}
		})
	}
}

func TestGCExpiredLeasePinAndHoldDoNotBlock(t *testing.T) {
	now := time.Unix(10_000, 0)
	state := gcState(now)
	expired := now.Add(-time.Second)
	state.Leases = []ArtifactLease{{LeaseID: "lease", ExpiresAt: expired}}
	state.Pins = []ArtifactPin{{PinID: "pin", Reason: "expired", ExpiresAt: &expired}}
	state.Holds = []RetentionHold{{HoldID: "hold", Reason: "expired", Until: &expired}}
	if decision := (GCPolicy{MinimumAge: time.Hour}).Evaluate(state, now); decision.Candidate == nil {
		t.Fatalf("expired protections must not block GC: %#v", decision.BlockedBy)
	}
}

func TestGCReceiptRejectsSubstitutionDuplicateAndUnknownOutcome(t *testing.T) {
	now := time.Unix(10_000, 0)
	plan, err := BuildGCPlan(GCPolicy{MinimumAge: time.Hour}, []GCArtifactState{gcState(now)}, now)
	if err != nil {
		t.Fatal(err)
	}
	candidate := plan.Candidates[0]

	badDigest := candidate.Ref.Digest
	other := identifiers.SHA256([]byte("other"))
	if badDigest.Equal(other) {
		t.Fatal("test digest collision")
	}
	for name, receipt := range map[string]GCReceipt{
		"substituted digest": {
			PlanID: plan.PlanID, CompletedAt: now,
			Results: []GCSweepResult{{Digest: other, ObjectPath: candidate.ObjectPath, Outcome: GCSweepDeleted}},
		},
		"unknown outcome": {
			PlanID: plan.PlanID, CompletedAt: now,
			Results: []GCSweepResult{{Digest: candidate.Ref.Digest, ObjectPath: candidate.ObjectPath, Outcome: "unknown"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateGCReceipt(plan, receipt); err == nil {
				t.Fatal("invalid GC receipt accepted")
			}
		})
	}

	// Two candidates with distinct paths allow duplicate-path receipt detection
	// without tripping the top-level result-count guard first.
	second := gcState(now)
	second.Ref.Digest = identifiers.SHA256([]byte("artifact-two"))
	second.ObjectPath = "sha256/bb/object"
	second.ObservedObjectVersion = "rv1:8:sha256:2e5b7d7e572b9d82ea29db72b05d3f5b80f6c8c6ce5d28da3177748fe2d98a6f"
	plan2, err := BuildGCPlan(GCPolicy{MinimumAge: time.Hour}, []GCArtifactState{gcState(now), second}, now)
	if err != nil {
		t.Fatal(err)
	}
	dup := GCReceipt{PlanID: plan2.PlanID, CompletedAt: now, Results: []GCSweepResult{
		{Digest: plan2.Candidates[0].Ref.Digest, ObjectPath: plan2.Candidates[0].ObjectPath, Outcome: GCSweepDeleted},
		{Digest: plan2.Candidates[0].Ref.Digest, ObjectPath: plan2.Candidates[0].ObjectPath, Outcome: GCSweepAlreadyAbsent},
	}}
	if err := ValidateGCReceipt(plan2, dup); err == nil {
		t.Fatal("duplicate GC receipt candidate accepted")
	}
}
