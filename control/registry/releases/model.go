// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package releases

import (
	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/identifiers"
	"time"
)

type EvidenceKind string

const (
	EvidenceSourceCommit             EvidenceKind = "source_commit"
	EvidenceResolvedConfig           EvidenceKind = "resolved_config"
	EvidenceDataset                  EvidenceKind = "dataset"
	EvidenceReferenceDatabase        EvidenceKind = "reference_database"
	EvidenceTrainingRun              EvidenceKind = "training_run"
	EvidenceCheckpoint               EvidenceKind = "checkpoint"
	EvidenceModelBundle              EvidenceKind = "model_bundle"
	EvidenceRuntimeBundle            EvidenceKind = "runtime_bundle"
	EvidenceKernelQualification      EvidenceKind = "kernel_qualification"
	EvidenceNumericalQualification   EvidenceKind = "numerical_qualification"
	EvidenceEvaluation               EvidenceKind = "evaluation"
	EvidenceSafety                   EvidenceKind = "safety"
	EvidenceScale                    EvidenceKind = "scale"
	EvidenceSBOM                     EvidenceKind = "sbom"
	EvidenceProvenance               EvidenceKind = "provenance"
	EvidenceSignature                EvidenceKind = "signature"
	EvidenceToolchain                EvidenceKind = "toolchain"
	EvidenceCheckpointResume         EvidenceKind = "checkpoint_resume"
	EvidencePerformance              EvidenceKind = "performance"
	EvidenceReliabilityQualification EvidenceKind = "reliability_qualification"
	EvidenceSLOApproval              EvidenceKind = "slo_approval"
	EvidenceAlertFireResolve         EvidenceKind = "alert_fire_resolve"
	EvidenceCostQualification        EvidenceKind = "cost_qualification"
	EvidenceSecurityQualification    EvidenceKind = "security_qualification"
	EvidenceLineage                  EvidenceKind = "lineage"
	EvidenceRollbackDrill            EvidenceKind = "rollback_drill"
	EvidenceH1001GQualification      EvidenceKind = "h100_1g_qualification"
	EvidenceH1008GQualification      EvidenceKind = "h100_8g_qualification"
	EvidenceVulnerabilityScan        EvidenceKind = "vulnerability_scan"
)

func (k EvidenceKind) Valid() bool {
	switch k {
	case EvidenceSourceCommit, EvidenceResolvedConfig, EvidenceDataset, EvidenceReferenceDatabase, EvidenceTrainingRun, EvidenceCheckpoint, EvidenceModelBundle, EvidenceRuntimeBundle, EvidenceKernelQualification, EvidenceNumericalQualification, EvidenceEvaluation, EvidenceSafety, EvidenceScale, EvidenceSBOM, EvidenceProvenance, EvidenceSignature, EvidenceToolchain, EvidenceCheckpointResume, EvidencePerformance, EvidenceReliabilityQualification, EvidenceSLOApproval, EvidenceAlertFireResolve, EvidenceCostQualification, EvidenceSecurityQualification, EvidenceLineage, EvidenceRollbackDrill, EvidenceH1001GQualification, EvidenceH1008GQualification, EvidenceVulnerabilityScan:
		return true
	}
	return false
}

type EvidenceNode struct {
	NodeID        string             `json:"node_id"`
	Kind          EvidenceKind       `json:"kind"`
	Artifact      artifacts.Ref      `json:"artifact"`
	SubjectDigest identifiers.Digest `json:"subject_digest"`
	PolicyDigest  identifiers.Digest `json:"policy_digest"`
	Passed        bool               `json:"passed"`
	Created       time.Time          `json:"created"`
}
type EvidenceEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`
}
type EvidenceGraph struct {
	ReleaseID     string             `json:"release_id"`
	SubjectDigest identifiers.Digest `json:"subject_digest"`
	Nodes         []EvidenceNode     `json:"nodes"`
	Edges         []EvidenceEdge     `json:"edges"`
	PolicyDigest  identifiers.Digest `json:"policy_digest"`
	PolicyEpoch   uint64             `json:"policy_epoch"`
}
type Release struct {
	ReleaseID           string             `json:"release_id"`
	ModelBundleDigest   identifiers.Digest `json:"model_bundle_digest"`
	EvidenceGraphDigest identifiers.Digest `json:"evidence_graph_digest"`
	Channel             string             `json:"channel"`
	Status              string             `json:"status"`
	ResourceVersion     uint64             `json:"resource_version"`
}
