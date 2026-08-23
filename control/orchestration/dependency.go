// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import "sort"

// Graph is the immutable dependency view of a validated workflow. It is built
// once by the compiler and answers readiness questions during reconciliation, so
// the reconciler never re-walks the raw stage list.
type Graph struct {
	order   []string
	byID    map[string]StageSpec
	parents map[string][]string
	// children is the reverse index. Without it, unblocking a stage after its
	// parent finished would be a scan of every stage in the workflow on every
	// completion, which is quadratic in the size of the graph.
	children map[string][]string
}

// NewGraph validates the workflow and indexes it. Validation is not optional
// here: Ready and Unblocked below assume an acyclic graph whose dependencies all
// resolve, and a cycle would make the topological order silently incomplete.
func NewGraph(workflow Workflow) (Graph, error) {
	if err := workflow.Validate(); err != nil {
		return Graph{}, err
	}
	if len(workflow.Stages) > MaximumStageCount {
		return Graph{}, exhausted("workflow_stage_bound", "workflow exceeds the maximum stage count")
	}
	graph := Graph{
		byID:     make(map[string]StageSpec, len(workflow.Stages)),
		parents:  make(map[string][]string, len(workflow.Stages)),
		children: make(map[string][]string, len(workflow.Stages)),
	}
	for _, stage := range workflow.Stages {
		graph.byID[stage.StageID] = stage
		parents := append([]string(nil), stage.Dependencies...)
		sort.Strings(parents)
		graph.parents[stage.StageID] = parents
	}
	for _, stage := range workflow.Stages {
		for _, parent := range stage.Dependencies {
			graph.children[parent] = append(graph.children[parent], stage.StageID)
		}
	}
	for parent := range graph.children {
		sort.Strings(graph.children[parent])
	}
	order, err := graph.topological()
	if err != nil {
		return Graph{}, err
	}
	graph.order = order
	return graph, nil
}

// topological returns a deterministic execution order. Kahn's algorithm with a
// sorted frontier rather than a plain queue: two runs of the same workflow must
// produce the same order, or the workflow definition digest would describe a
// plan the compiler cannot reproduce.
func (graph Graph) topological() ([]string, error) {
	remaining := make(map[string]int, len(graph.byID))
	frontier := make([]string, 0, len(graph.byID))
	for id, parents := range graph.parents {
		remaining[id] = len(parents)
		if len(parents) == 0 {
			frontier = append(frontier, id)
		}
	}
	sort.Strings(frontier)
	order := make([]string, 0, len(graph.byID))
	for len(frontier) > 0 {
		id := frontier[0]
		frontier = frontier[1:]
		order = append(order, id)
		released := make([]string, 0, len(graph.children[id]))
		for _, child := range graph.children[id] {
			remaining[child]--
			if remaining[child] == 0 {
				released = append(released, child)
			}
		}
		if len(released) > 0 {
			frontier = append(frontier, released...)
			sort.Strings(frontier)
		}
	}
	if len(order) != len(graph.byID) {
		// Workflow.Validate already rejects cycles, so reaching this is a
		// disagreement between the two walks rather than bad input.
		return nil, invalid("workflow_cycle", "workflow dependency graph contains a cycle", nil)
	}
	return order, nil
}

// Order returns the deterministic topological order.
func (graph Graph) Order() []string { return append([]string(nil), graph.order...) }

// Len reports the number of stages.
func (graph Graph) Len() int { return len(graph.byID) }

// Stage returns one stage specification.
func (graph Graph) Stage(id string) (StageSpec, bool) {
	stage, ok := graph.byID[id]
	return stage, ok
}

// Dependencies returns the sorted parents of a stage.
func (graph Graph) Dependencies(id string) []string {
	return append([]string(nil), graph.parents[id]...)
}

// Dependents returns the sorted children of a stage.
func (graph Graph) Dependents(id string) []string {
	return append([]string(nil), graph.children[id]...)
}

// Ready returns the stages whose dependencies have all succeeded, in
// topological order.
//
// Only StageSucceeded releases a dependent. A failed or cancelled parent leaves
// its children blocked forever, which is the intended outcome: a stage consumes
// its parents' outputs, and those outputs do not exist. Cancelling the run is
// what clears them, not this function.
func (graph Graph) Ready(states map[string]StageState) []string {
	ready := make([]string, 0, len(graph.order))
	for _, id := range graph.order {
		if states[id] != StageBlocked {
			continue
		}
		if graph.satisfied(id, states) {
			ready = append(ready, id)
		}
	}
	return ready
}

// Unblocked returns the children of one just-succeeded stage that are now
// ready. This is the incremental form of Ready for a reconciler holding a single
// completion, and it exists so completing one stage costs the size of that
// stage's fan-out rather than the size of the workflow.
func (graph Graph) Unblocked(completed string, states map[string]StageState) []string {
	if states[completed] != StageSucceeded {
		return nil
	}
	ready := make([]string, 0, len(graph.children[completed]))
	for _, child := range graph.children[completed] {
		if states[child] != StageBlocked {
			continue
		}
		if graph.satisfied(child, states) {
			ready = append(ready, child)
		}
	}
	return ready
}

// CompletionScope returns every stage whose durable state Unblocked reads after
// one stage completes: the stage itself, its children, and those children's
// other parents.
//
// It exists so a caller can fetch exactly what the decision needs instead of the
// whole run. The set is bounded by the graph's shape rather than its size, which
// is what keeps completing one stage of a wide fan-out from costing a full scan.
func (graph Graph) CompletionScope(completed string) []string {
	children := graph.children[completed]
	scope := make(map[string]bool, len(children)*2+1)
	scope[completed] = true
	for _, child := range children {
		scope[child] = true
		for _, parent := range graph.parents[child] {
			scope[parent] = true
		}
	}
	ids := make([]string, 0, len(scope))
	for id := range scope {
		ids = append(ids, id)
	}
	// Sorted so a repository sees a stable key order, which keeps a SQL
	// implementation's query plan and lock order deterministic.
	sort.Strings(ids)
	return ids
}

func (graph Graph) satisfied(id string, states map[string]StageState) bool {
	for _, parent := range graph.parents[id] {
		if states[parent] != StageSucceeded {
			return false
		}
	}
	return true
}

// InitialStates returns the state map a freshly compiled workflow starts from:
// every stage blocked. Roots become ready on the first Ready call because they
// have no parents to satisfy, so there is no special case for them here.
func (graph Graph) InitialStates() map[string]StageState {
	states := make(map[string]StageState, len(graph.byID))
	for id := range graph.byID {
		states[id] = StageBlocked
	}
	return states
}
