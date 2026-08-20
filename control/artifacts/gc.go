// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package artifacts

import (
	"sort"
	"strings"
	"time"

	"go.mindclade.dev/libs/go/identifiers"
)

type GCBlockReason string

const (
	GCReachable         GCBlockReason = "reachable"
	GCActiveLease       GCBlockReason = "active_lease"
	GCAdministrativePin GCBlockReason = "administrative_pin"
	GCRetentionHold     GCBlockReason = "retention_hold"
	GCRetentionWindow   GCBlockReason = "retention_window"
	GCLocationInvalid   GCBlockReason = "location_invalid"
)

type ArtifactReachability struct{ DurableReferences, ReleaseEvidenceReferences, AuditReferences uint64 }

func (r ArtifactReachability) Reachable() bool {
	return r.DurableReferences+r.ReleaseEvidenceReferences+r.AuditReferences > 0
}

type ArtifactLease struct {
	LeaseID   string
	ExpiresAt time.Time
}

func (l ArtifactLease) Active(now time.Time) bool { return l.LeaseID != "" && l.ExpiresAt.After(now) }

type ArtifactPin struct {
	PinID, Reason string
	ExpiresAt     *time.Time
}

func (p ArtifactPin) Active(now time.Time) bool {
	return p.PinID != "" && (p.ExpiresAt == nil || p.ExpiresAt.After(now))
}

type RetentionHold struct {
	HoldID, Reason string
	Until          *time.Time
}

func (h RetentionHold) Active(now time.Time) bool {
	return h.HoldID != "" && (h.Until == nil || h.Until.After(now))
}

type GCArtifactState struct {
	Ref          Ref
	CreatedAt    time.Time
	Reachability ArtifactReachability
	Leases       []ArtifactLease
	Pins         []ArtifactPin
	Holds        []RetentionHold
	// ObjectPath is the exact relative key observed when the plan was built.
	// The byte plane must never reconstruct it from an artifact URI.
	ObjectPath            string
	ObservedObjectVersion string
}

type GCCandidate struct {
	Ref                               Ref
	ObjectPath, ObservedObjectVersion string
	EligibleAt                        time.Time
}
type GCDecision struct {
	Candidate *GCCandidate
	BlockedBy []GCBlockReason
}
type GCPolicy struct {
	MinimumAge       time.Duration
	MinimumAgeByKind map[string]time.Duration
}

func (p GCPolicy) age(kind string) time.Duration {
	if v, ok := p.MinimumAgeByKind[kind]; ok {
		return v
	}
	return p.MinimumAge
}

func validGCObjectPath(value string) bool {
	return value != "" && len(value) <= 1024 && value == strings.TrimSpace(value) &&
		!strings.HasPrefix(value, "/") && !strings.HasSuffix(value, "/") &&
		!strings.Contains(value, "//") && !strings.Contains(value, "\\") && !strings.ContainsRune(value, '\x00') &&
		value != "." && value != ".." && !strings.Contains(value, "/../") && !strings.HasPrefix(value, "../")
}

func (p GCPolicy) Evaluate(state GCArtifactState, now time.Time) GCDecision {
	reasons := make([]GCBlockReason, 0, 6)
	if state.Reachability.Reachable() {
		reasons = append(reasons, GCReachable)
	}
	for _, lease := range state.Leases {
		if lease.Active(now) {
			reasons = append(reasons, GCActiveLease)
			break
		}
	}
	for _, pin := range state.Pins {
		if pin.Active(now) {
			reasons = append(reasons, GCAdministrativePin)
			break
		}
	}
	for _, hold := range state.Holds {
		if hold.Active(now) {
			reasons = append(reasons, GCRetentionHold)
			break
		}
	}
	eligibleAt := state.CreatedAt.Add(p.age(state.Ref.LogicalKind))
	if state.CreatedAt.IsZero() || now.Before(eligibleAt) {
		reasons = append(reasons, GCRetentionWindow)
	}
	if !validGCObjectPath(state.ObjectPath) || state.ObservedObjectVersion == "" || len(state.ObservedObjectVersion) > 512 {
		reasons = append(reasons, GCLocationInvalid)
	}
	if len(reasons) > 0 {
		return GCDecision{BlockedBy: reasons}
	}
	return GCDecision{Candidate: &GCCandidate{Ref: state.Ref, ObjectPath: state.ObjectPath, ObservedObjectVersion: state.ObservedObjectVersion, EligibleAt: eligibleAt}}
}

type GCPlan struct {
	PlanID     identifiers.Digest
	CreatedAt  time.Time
	Candidates []GCCandidate
}

func BuildGCPlan(policy GCPolicy, states []GCArtifactState, now time.Time) (GCPlan, error) {
	candidates := make([]GCCandidate, 0, len(states))
	for _, state := range states {
		if err := state.Ref.Validate(); err != nil {
			return GCPlan{}, err
		}
		if d := policy.Evaluate(state, now); d.Candidate != nil {
			candidates = append(candidates, *d.Candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Ref.Digest.Equal(candidates[j].Ref.Digest) {
			return candidates[i].ObjectPath < candidates[j].ObjectPath
		}
		return candidates[i].Ref.Digest.String() < candidates[j].Ref.Digest.String()
	})
	payload := make([]byte, 0, len(candidates)*160)
	for _, c := range candidates {
		payload = append(payload, []byte(c.Ref.Digest.String()+"\x00"+c.ObjectPath+"\x00"+c.ObservedObjectVersion+"\n")...)
	}
	return GCPlan{PlanID: identifiers.SHA256(payload), CreatedAt: now, Candidates: candidates}, nil
}

type GCSweepOutcome string

const (
	GCSweepDeleted       GCSweepOutcome = "deleted"
	GCSweepAlreadyAbsent GCSweepOutcome = "already_absent"
	GCSweepStale         GCSweepOutcome = "stale"
)

type GCSweepResult struct {
	Digest     identifiers.Digest
	ObjectPath string
	Outcome    GCSweepOutcome
}
type GCReceipt struct {
	PlanID      identifiers.Digest
	CompletedAt time.Time
	Results     []GCSweepResult
}

// ValidateGCReceipt proves the byte plane reported exactly one outcome for every
// planned object and did not substitute a different digest/path.
func ValidateGCReceipt(plan GCPlan, receipt GCReceipt) error {
	if !receipt.PlanID.Equal(plan.PlanID) || receipt.CompletedAt.IsZero() || len(receipt.Results) != len(plan.Candidates) {
		return invalid("gc_receipt_invalid", "GC receipt does not match plan", nil)
	}
	expected := make(map[string]identifiers.Digest, len(plan.Candidates))
	for _, c := range plan.Candidates {
		expected[c.ObjectPath] = c.Ref.Digest
	}
	seen := make(map[string]struct{}, len(receipt.Results))
	for _, r := range receipt.Results {
		digest, ok := expected[r.ObjectPath]
		if !ok || !digest.Equal(r.Digest) {
			return invalid("gc_receipt_candidate_mismatch", "GC receipt candidate mismatch", nil)
		}
		if _, dup := seen[r.ObjectPath]; dup {
			return invalid("gc_receipt_duplicate", "GC receipt contains duplicate candidate", nil)
		}
		seen[r.ObjectPath] = struct{}{}
		if r.Outcome != GCSweepDeleted && r.Outcome != GCSweepAlreadyAbsent && r.Outcome != GCSweepStale {
			return invalid("gc_receipt_outcome_invalid", "GC receipt outcome invalid", nil)
		}
	}
	return nil
}
