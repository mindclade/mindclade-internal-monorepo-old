// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package releases

import "go.mindclade.dev/libs/go/identifiers"

type PromotionPolicy struct {
	Required           []EvidenceKind
	RequireAllPassed   bool
	ActivePolicyDigest identifiers.Digest
	ActivePolicyEpoch  uint64
}

func (p PromotionPolicy) Evaluate(g EvidenceGraph) error {
	if err := g.Validate(); err != nil {
		return err
	}
	if !p.ActivePolicyDigest.Valid() || p.ActivePolicyEpoch == 0 {
		return invalid("release_policy_unconfigured", "release promotion policy authority is not configured", nil)
	}
	if !g.PolicyDigest.Equal(p.ActivePolicyDigest) || g.PolicyEpoch != p.ActivePolicyEpoch {
		return invalid("release_policy_mismatch", "release evidence was not evaluated under the active policy", nil)
	}
	if len(p.Required) > len(g.Nodes) || len(p.Required) > MaximumEvidenceNodes {
		return invalid("release_policy_size", "release policy required-kind count is outside bounds", nil)
	}
	required := make(map[EvidenceKind]struct{}, len(p.Required))
	for _, kind := range p.Required {
		if !kind.Valid() {
			return invalid("release_policy_kind", "release policy contains an invalid evidence kind", nil)
		}
		if _, exists := required[kind]; exists {
			return invalid("release_policy_duplicate", "release policy contains a duplicate required kind", nil)
		}
		required[kind] = struct{}{}
	}
	present := map[EvidenceKind]uint32{}
	for _, n := range g.Nodes {
		if !p.RequireAllPassed || n.Passed {
			present[n.Kind]++
		}
		if p.RequireAllPassed && !n.Passed {
			return invalid("release_evidence_failed", "release evidence graph contains failed evidence", nil)
		}
	}
	for _, k := range p.Required {
		if present[k] == 0 {
			return invalid("release_evidence_missing", "release is missing mandatory evidence: "+string(k), nil)
		}
		if present[k] != 1 {
			return invalid("release_evidence_duplicate", "release contains duplicate mandatory evidence: "+string(k), nil)
		}
	}
	return nil
}
func ProductionPolicy() PromotionPolicy {
	return PromotionPolicy{RequireAllPassed: true, Required: []EvidenceKind{
		EvidenceSourceCommit,
		EvidenceResolvedConfig,
		EvidenceDataset,
		EvidenceTrainingRun,
		EvidenceCheckpoint,
		EvidenceCheckpointResume,
		EvidenceModelBundle,
		EvidenceRuntimeBundle,
		EvidenceKernelQualification,
		EvidenceNumericalQualification,
		EvidenceEvaluation,
		EvidenceSafety,
		EvidenceScale,
		EvidencePerformance,
		EvidenceReliabilityQualification,
		EvidenceSLOApproval,
		EvidenceAlertFireResolve,
		EvidenceCostQualification,
		EvidenceSecurityQualification,
		EvidenceVulnerabilityScan,
		EvidenceLineage,
		EvidenceRollbackDrill,
		EvidenceH1001GQualification,
		EvidenceH1008GQualification,
		EvidenceSBOM,
		EvidenceProvenance,
		EvidenceSignature,
		EvidenceToolchain,
	}}
}
