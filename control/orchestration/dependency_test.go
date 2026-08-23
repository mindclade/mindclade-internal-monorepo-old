// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"testing"
	"time"

	"go.mindclade.dev/control/runtime_authority"
	"go.mindclade.dev/libs/go/identifiers"
)

var testStart = time.Date(2026, time.August, 23, 6, 0, 0, 0, time.UTC)

func testBudget() runtime_authority.ExecutionBudget {
	return runtime_authority.ExecutionBudget{
		CPUMillis: 1000, ResidentMemoryBytes: 1 << 20,
		OpenFileDescriptors: 16, CPUWorkerThreads: 1,
	}
}

func testID(t *testing.T, kind string) string {
	t.Helper()
	id, err := identifiers.NewID(identifiers.MustParseKind(kind))
	if err != nil {
		t.Fatalf("new %s id: %v", kind, err)
	}
	return id.String()
}

func testStage(t *testing.T, id, operation string, dependencies ...string) StageSpec {
	t.Helper()
	return StageSpec{
		StageID:              id,
		Kind:                 StagePreprocess,
		Operation:            operation,
		OutputNamespace:      "raw",
		ResolvedConfigDigest: identifiers.SHA256String("config"),
		Budget:               testBudget(),
		Timeout:              time.Minute,
		MaximumAttempts:      3,
		Dependencies:         dependencies,
	}
}

// A diamond exercises both fan-out and fan-in: the join must wait for BOTH
// parents, which a per-parent readiness check would get wrong.
func diamond(t *testing.T) (Graph, [4]string) {
	t.Helper()
	root := testID(t, "stage")
	left := testID(t, "stage")
	right := testID(t, "stage")
	join := testID(t, "stage")
	graph, err := NewGraph(Workflow{Stages: []StageSpec{
		testStage(t, root, "fetch"),
		testStage(t, left, "msa", root),
		testStage(t, right, "template", root),
		testStage(t, join, "features", left, right),
	}})
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	return graph, [4]string{root, left, right, join}
}

func TestGraphOrderRespectsDependencies(t *testing.T) {
	graph, ids := diamond(t)
	position := map[string]int{}
	for index, id := range graph.Order() {
		position[id] = index
	}
	if len(position) != 4 {
		t.Fatalf("expected 4 ordered stages, got %d", len(position))
	}
	for _, child := range []string{ids[1], ids[2]} {
		if position[ids[0]] >= position[child] {
			t.Fatalf("root must precede %s", child)
		}
	}
	if position[ids[1]] >= position[ids[3]] || position[ids[2]] >= position[ids[3]] {
		t.Fatal("both parents must precede the join")
	}
}

// The definition digest is computed over the plan, so the order must not depend
// on map iteration. A flaky order would make an identical workflow hash two ways.
func TestGraphOrderIsDeterministic(t *testing.T) {
	graph, _ := diamond(t)
	first := graph.Order()
	for range 16 {
		rebuilt, err := NewGraph(Workflow{Stages: graph.workflowStages()})
		if err != nil {
			t.Fatalf("rebuild: %v", err)
		}
		next := rebuilt.Order()
		for index := range first {
			if first[index] != next[index] {
				t.Fatalf("order drifted at %d: %s != %s", index, first[index], next[index])
			}
		}
	}
}

func TestReadyReleasesOnlyAfterEveryParentSucceeded(t *testing.T) {
	graph, ids := diamond(t)
	root, left, right, join := ids[0], ids[1], ids[2], ids[3]

	states := graph.InitialStates()
	ready := graph.Ready(states)
	if len(ready) != 1 || ready[0] != root {
		t.Fatalf("only the root is initially ready, got %v", ready)
	}

	states[root] = StageSucceeded
	states[left] = StageBlocked
	states[right] = StageBlocked
	ready = graph.Ready(states)
	if len(ready) != 2 {
		t.Fatalf("both children release together, got %v", ready)
	}

	// One parent done is not enough for the join.
	states[left] = StageSucceeded
	states[right] = StageRunning
	for _, id := range graph.Ready(states) {
		if id == join {
			t.Fatal("the join must wait for its second parent")
		}
	}

	states[right] = StageSucceeded
	ready = graph.Ready(states)
	if len(ready) != 1 || ready[0] != join {
		t.Fatalf("the join releases once both parents succeed, got %v", ready)
	}
}

// A failed parent leaves children blocked rather than ready: the outputs they
// consume do not exist. Cancelling the run is what clears them.
func TestFailedParentDoesNotReleaseChildren(t *testing.T) {
	graph, ids := diamond(t)
	states := graph.InitialStates()
	states[ids[0]] = StageFailed
	if ready := graph.Ready(states); len(ready) != 0 {
		t.Fatalf("a failed parent must release nothing, got %v", ready)
	}
}

func TestUnblockedMatchesReadyForOneCompletion(t *testing.T) {
	graph, ids := diamond(t)
	states := graph.InitialStates()
	states[ids[0]] = StageSucceeded
	unblocked := graph.Unblocked(ids[0], states)
	if len(unblocked) != 2 {
		t.Fatalf("root completion unblocks both children, got %v", unblocked)
	}
	// A stage that has not succeeded unblocks nothing, so a status that arrives
	// out of order cannot start downstream work.
	states[ids[0]] = StageRunning
	if got := graph.Unblocked(ids[0], states); len(got) != 0 {
		t.Fatalf("a running stage unblocks nothing, got %v", got)
	}
}

func TestGraphRejectsAnUnknownDependency(t *testing.T) {
	stage := testStage(t, testID(t, "stage"), "fetch", testID(t, "stage"))
	if _, err := NewGraph(Workflow{Stages: []StageSpec{stage}}); err == nil {
		t.Fatal("a dependency on an unknown stage must be rejected")
	}
}

// workflowStages rebuilds the stage list from a graph's own index so a test can
// re-derive an identical graph. It lives here rather than in the production file
// because nothing outside a test needs it.
func (graph Graph) workflowStages() []StageSpec {
	stages := make([]StageSpec, 0, len(graph.order))
	for _, id := range graph.order {
		stages = append(stages, graph.byID[id])
	}
	return stages
}
