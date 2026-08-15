// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

type Workflow struct{ Stages []StageSpec }

func (w Workflow) Validate() error {
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
