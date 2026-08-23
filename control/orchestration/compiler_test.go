// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"testing"
	"time"

	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/identifiers"
)

func testWorkflowID(t *testing.T) identifiers.ID {
	t.Helper()
	id, err := identifiers.NewID(identifiers.MustParseKind("workflow"))
	if err != nil {
		t.Fatalf("new workflow id: %v", err)
	}
	return id
}

// The digest must be reproducible, or a plan cannot be addressed by what it
// does and the compiler could not recognize a resubmission of the same work.
func TestCompileProducesAStableDefinitionDigest(t *testing.T) {
	stages := []StageSpec{
		testStage(t, testID(t, "stage"), "fetch"),
		testStage(t, testID(t, "stage"), "msa"),
	}
	id := testWorkflowID(t)
	first, err := Compile(CompileRequest{Name: "pipeline", Stages: stages}, id)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	second, err := Compile(CompileRequest{Name: "pipeline", Stages: stages}, id)
	if err != nil {
		t.Fatalf("recompile: %v", err)
	}
	if !first.Workflow.DefinitionDigest.Equal(second.Workflow.DefinitionDigest) {
		t.Fatal("compiling identical requests must reproduce one digest")
	}
}

// Submission order is not part of the plan, so reordering the same stages must
// not look like a different computation.
func TestCompileDigestIgnoresSubmissionOrder(t *testing.T) {
	first := testStage(t, testID(t, "stage"), "fetch")
	second := testStage(t, testID(t, "stage"), "msa")
	id := testWorkflowID(t)
	forward, err := Compile(CompileRequest{Name: "pipeline", Stages: []StageSpec{first, second}}, id)
	if err != nil {
		t.Fatalf("compile forward: %v", err)
	}
	reversed, err := Compile(CompileRequest{Name: "pipeline", Stages: []StageSpec{second, first}}, id)
	if err != nil {
		t.Fatalf("compile reversed: %v", err)
	}
	if !forward.Workflow.DefinitionDigest.Equal(reversed.Workflow.DefinitionDigest) {
		t.Fatal("stage submission order must not change the definition digest")
	}
}

// Every field that changes what executes must move the digest, or two different
// computations could share one identity.
func TestCompileDigestCoversExecutionRelevantFields(t *testing.T) {
	base := testStage(t, testID(t, "stage"), "fetch")
	id := testWorkflowID(t)
	baseline, err := Compile(CompileRequest{Name: "pipeline", Stages: []StageSpec{base}}, id)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mutations := map[string]func(*StageSpec){
		"operation":     func(s *StageSpec) { s.Operation = "template" },
		"kind":          func(s *StageSpec) { s.Kind = StageTraining },
		"namespace":     func(s *StageSpec) { s.OutputNamespace = "derived" },
		"config digest": func(s *StageSpec) { s.ResolvedConfigDigest = identifiers.SHA256String("other") },
		"timeout":       func(s *StageSpec) { s.Timeout = 2 * time.Minute },
		"attempts":      func(s *StageSpec) { s.MaximumAttempts = 7 },
		"cpu budget":    func(s *StageSpec) { s.Budget.CPUMillis = 2000 },
		"gpu estimate":  func(s *StageSpec) { s.Budget.GPUMemoryEstimateBytes = 1 << 30 },
		"inputs": func(s *StageSpec) {
			s.Inputs = []artifacts.Ref{{
				Digest: identifiers.SHA256String("input"), SizeBytes: 8,
				MediaType: "application/octet-stream", LogicalKind: "raw", SchemaVersion: 1,
			}}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			stage := base
			mutate(&stage)
			changed, err := Compile(CompileRequest{Name: "pipeline", Stages: []StageSpec{stage}}, id)
			if err != nil {
				t.Fatalf("compile mutated: %v", err)
			}
			if changed.Workflow.DefinitionDigest.Equal(baseline.Workflow.DefinitionDigest) {
				t.Fatalf("changing %s must change the definition digest", name)
			}
		})
	}
}

// Identity names a plan; it does not define one. Two runs of the same pipeline
// must be recognizable as the same computation.
func TestCompileDigestExcludesWorkflowIdentity(t *testing.T) {
	stages := []StageSpec{testStage(t, testID(t, "stage"), "fetch")}
	first, err := Compile(CompileRequest{Name: "alpha", Stages: stages}, testWorkflowID(t))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	second, err := Compile(CompileRequest{Name: "beta", Stages: stages}, testWorkflowID(t))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !first.Workflow.DefinitionDigest.Equal(second.Workflow.DefinitionDigest) {
		t.Fatal("workflow id and name must not participate in the definition digest")
	}
}

func TestCompileRejectsInvalidInput(t *testing.T) {
	valid := testStage(t, testID(t, "stage"), "fetch")
	cases := map[string]struct {
		name  string
		stage func() StageSpec
	}{
		"empty name":     {name: "", stage: func() StageSpec { return valid }},
		"bad operation":  {name: "pipeline", stage: func() StageSpec { s := valid; s.Operation = "bad operation"; return s }},
		"zero attempts":  {name: "pipeline", stage: func() StageSpec { s := valid; s.MaximumAttempts = 0; return s }},
		"self dependent": {name: "pipeline", stage: func() StageSpec { s := valid; s.Dependencies = []string{s.StageID}; return s }},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Compile(CompileRequest{Name: testCase.name, Stages: []StageSpec{testCase.stage()}}, testWorkflowID(t))
			if err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestCompileRejectsAnEmptyWorkflow(t *testing.T) {
	if _, err := Compile(CompileRequest{Name: "pipeline"}, testWorkflowID(t)); err == nil {
		t.Fatal("a workflow with no stages must be rejected")
	}
}
