// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"mindclade.internal/control/artifacts"
	"mindclade.internal/control/runtime_authority"
	"mindclade.internal/libs/go/identifiers"
	"time"
)

type StageKind string

const (
	StageIngestion          StageKind = "ingestion"
	StageCurate             StageKind = "curate"
	StagePreprocess         StageKind = "preprocess"
	StageReferenceBuild     StageKind = "reference_build"
	StageBatchInference     StageKind = "batch_inference"
	StageEvaluation         StageKind = "evaluation"
	StageTraining           StageKind = "training"
	StageCheckpointTransfer StageKind = "checkpoint_transfer"
	StageArtifactTransfer   StageKind = "artifact_transfer"
	StageRollout            StageKind = "rollout"
	StageSimulation         StageKind = "simulation"
)

func (k StageKind) Valid() bool {
	switch k {
	case StageIngestion, StageCurate, StagePreprocess, StageReferenceBuild, StageBatchInference, StageEvaluation, StageTraining, StageCheckpointTransfer, StageArtifactTransfer, StageRollout, StageSimulation:
		return true
	}
	return false
}

type StageSpec struct {
	StageID                 string
	Kind                    StageKind
	Operation               string
	Inputs                  []artifacts.Ref
	OutputNamespace         string
	ResolvedConfigDigest    identifiers.Digest
	ReferenceSnapshotDigest identifiers.Digest
	Budget                  runtime_authority.ExecutionBudget
	Timeout                 time.Duration
	MaximumAttempts         uint32
	Dependencies            []string
}

func (s StageSpec) Validate() error {
	if _, err := identifiers.ParseID(s.StageID); err != nil {
		return invalid("stage_id_invalid", "stage id must be canonical", err)
	}
	if !s.Kind.Valid() || s.Operation == "" || s.OutputNamespace == "" || !s.ResolvedConfigDigest.Valid() || s.Timeout <= 0 || s.MaximumAttempts == 0 {
		return invalid("stage_spec_invalid", "stage kind, operation, output namespace, config digest, timeout, and attempts are required", nil)
	}
	if err := s.Budget.Validate(); err != nil {
		return err
	}
	for _, a := range s.Inputs {
		if err := a.Validate(); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, d := range s.Dependencies {
		if d == s.StageID || seen[d] {
			return invalid("stage_dependency_invalid", "stage dependencies must be unique and cannot reference self", nil)
		}
		if _, err := identifiers.ParseID(d); err != nil {
			return invalid("stage_dependency_id_invalid", "stage dependency id must be canonical", err)
		}
		seen[d] = true
	}
	return nil
}

type StageAttempt struct {
	RunID             string
	JobID             string
	Spec              StageSpec
	Attempt           uint32
	FencingToken      uint64
	ExecutionTicketID string
}

func (a StageAttempt) Validate() error {
	if err := a.Spec.Validate(); err != nil {
		return err
	}
	if a.Attempt == 0 || a.Attempt > a.Spec.MaximumAttempts || a.FencingToken == 0 {
		return invalid("stage_attempt_invalid", "stage attempt or fencing token is invalid", nil)
	}
	return nil
}
