// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import "go.mindclade.dev/libs/go/identifiers"

// MaximumStageCount bounds one workflow graph. The graph is walked on every
// reconcile and its digest is recomputed on every compile, so an unbounded
// stage list is an unbounded amount of work per claimed item.
const MaximumStageCount = 4096

// Workflow is an immutable acyclic stage graph, mirroring
// mindclade.orchestration.v1.Workflow.
//
// Identity is separate from validity on purpose. Validate checks only the graph,
// because control/ingestion builds a Workflow from a stage list to reuse that
// check and has no workflow identity to supply. ValidateIdentity is the
// additional contract a durable, published workflow must meet, and the compiler
// is what applies it.
type Workflow struct {
	ID               string
	Name             string
	Stages           []StageSpec
	DefinitionDigest identifiers.Digest
	SchemaVersion    uint32
}

// ValidateIdentity checks the fields that make a workflow addressable. It is
// deliberately not called from Validate: tightening Validate would break every
// caller that legitimately holds only a stage list.
func (w Workflow) ValidateIdentity() error {
	if err := validateID(w.ID, "workflow", "workflow_id"); err != nil {
		return err
	}
	if err := validateBoundedName(w.Name, "workflow_name", MaximumOutputNamespaceLength); err != nil {
		return err
	}
	if !w.DefinitionDigest.Valid() {
		return invalid("workflow_definition_digest_invalid", "workflow definition digest is required", nil)
	}
	if w.SchemaVersion == 0 {
		return invalid("workflow_schema_version_invalid", "workflow schema version is required", nil)
	}
	return w.Validate()
}

func (w Workflow) Validate() error {
	if len(w.Stages) == 0 {
		return invalid("workflow_empty", "workflow contains no stages", nil)
	}
	if len(w.Stages) > MaximumStageCount {
		return exhausted("workflow_stage_bound", "workflow exceeds the maximum stage count")
	}
	byID := map[string]StageSpec{}
	for _, s := range w.Stages {
		if err := s.Validate(); err != nil {
			return err
		}
		if _, ok := byID[s.StageID]; ok {
			return invalid("duplicate_stage_id", "workflow contains duplicate stage id", nil)
		}
		byID[s.StageID] = s
	}
	state := map[string]uint8{}
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case 1:
			return invalid("workflow_cycle", "workflow dependency graph contains a cycle", nil)
		case 2:
			return nil
		}
		s, ok := byID[id]
		if !ok {
			return invalid("workflow_dependency_missing", "workflow dependency references an unknown stage", nil)
		}
		state[id] = 1
		for _, d := range s.Dependencies {
			if err := visit(d); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	for id := range byID {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
