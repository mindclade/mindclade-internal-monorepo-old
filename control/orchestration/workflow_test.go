// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"go.mindclade.dev/control/runtime_authority"
	"go.mindclade.dev/libs/go/identifiers"
	"testing"
	"time"
)

func TestWorkflowRejectsCycle(t *testing.T) {
	a, _ := identifiers.NewID(identifiers.MustParseKind("stage"))
	b, _ := identifiers.NewID(identifiers.MustParseKind("stage"))
	cfg := identifiers.SHA256([]byte("c"))
	budget := runtime_authority.ExecutionBudget{CPUMillis: 1, ResidentMemoryBytes: 1, OpenFileDescriptors: 1, CPUWorkerThreads: 1}
	w := Workflow{Stages: []StageSpec{{StageID: a.String(), Kind: StagePreprocess, Operation: "msa", OutputNamespace: "x", ResolvedConfigDigest: cfg, Budget: budget, Timeout: time.Second, MaximumAttempts: 1, Dependencies: []string{b.String()}}, {StageID: b.String(), Kind: StagePreprocess, Operation: "template", OutputNamespace: "x", ResolvedConfigDigest: cfg, Budget: budget, Timeout: time.Second, MaximumAttempts: 1, Dependencies: []string{a.String()}}}}
	if err := w.Validate(); err == nil {
		t.Fatal("expected cycle rejection")
	}
}
