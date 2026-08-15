// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package releases

import (
	"mindclade.internal/control/artifacts"
	"mindclade.internal/libs/go/identifiers"
	"time"
)

type EvidenceKind string

const (
	EvidenceSourceCommit           EvidenceKind = "source_commit"
	EvidenceResolvedConfig         EvidenceKind = "resolved_config"
	EvidenceDataset                EvidenceKind = "dataset"
	EvidenceReferenceDatabase      EvidenceKind = "reference_database"
	EvidenceTrainingRun            EvidenceKind = "training_run"
	EvidenceCheckpoint             EvidenceKind = "checkpoint"
	EvidenceModelBundle            EvidenceKind = "model_bundle"
	EvidenceRuntimeBundle          EvidenceKind = "runtime_bundle"
	EvidenceKernelQualification    EvidenceKind = "kernel_qualification"
	EvidenceNumericalQualification EvidenceKind = "numerical_qualification"
	EvidenceEvaluation             EvidenceKind = "evaluation"
	EvidenceSafety                 EvidenceKind = "safety"
	EvidenceScale                  EvidenceKind = "scale"
	EvidenceSBOM                   EvidenceKind = "sbom"
	EvidenceProvenance             EvidenceKind = "provenance"
	EvidenceSignature              EvidenceKind = "signature"
	EvidenceToolchain              EvidenceKind = "toolchain"
)

func (k EvidenceKind) Valid() bool {
	switch k {
	case EvidenceSourceCommit, EvidenceResolvedConfig, EvidenceDataset, EvidenceReferenceDatabase, EvidenceTrainingRun, EvidenceCheckpoint, EvidenceModelBundle, EvidenceRuntimeBundle, EvidenceKernelQualification, EvidenceNumericalQualification, EvidenceEvaluation, EvidenceSafety, EvidenceScale, EvidenceSBOM, EvidenceProvenance, EvidenceSignature, EvidenceToolchain:
		return true
	}
	return false
}

type EvidenceNode struct {
	NodeID        string
	Kind          EvidenceKind
	Artifact      artifacts.Ref
	SubjectDigest identifiers.Digest
	PolicyDigest  identifiers.Digest
	Passed        bool
	Created       time.Time
}
type EvidenceEdge struct {
	From     string
	To       string
	Relation string
}
type EvidenceGraph struct {
	ReleaseID     string
	SubjectDigest identifiers.Digest
	Nodes         []EvidenceNode
	Edges         []EvidenceEdge
	PolicyDigest  identifiers.Digest
	PolicyEpoch   uint64
}
type Release struct {
	ReleaseID           string
	ModelBundleDigest   identifiers.Digest
	EvidenceGraphDigest identifiers.Digest
	Channel             string
	Status              string
	ResourceVersion     uint64
}
