// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package ingestion

import (
	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/runtime_authority"
	"go.mindclade.dev/libs/go/identifiers"
	"testing"
	"time"
)

func id(t *testing.T, kind string) string {
	t.Helper()
	k, err := identifiers.ParseKind(kind)
	if err != nil {
		t.Fatal(err)
	}
	v, err := identifiers.NewIDAt(k, time.UnixMilli(1))
	if err != nil {
		t.Fatal(err)
	}
	return v.String()
}
func TestCompileCanonicalWorkload(t *testing.T) {
	now := time.Unix(1000, 0)
	config := identifiers.SHA256([]byte("config"))
	ref := artifacts.Ref{Digest: identifiers.SHA256([]byte("raw")), SizeBytes: 3, MediaType: "application/octet-stream", LogicalKind: "raw_source", SchemaVersion: 1}
	spec := orchestration.StageSpec{StageID: id(t, "stage"), Kind: orchestration.StageIngestion, Operation: "fetch", Inputs: []artifacts.Ref{ref}, OutputNamespace: "raw", ResolvedConfigDigest: config, Budget: runtime_authority.ExecutionBudget{CPUMillis: 1000, ResidentMemoryBytes: 1024, OpenFileDescriptors: 16, CPUWorkerThreads: 1}, Timeout: time.Minute, MaximumAttempts: 3}
	p := Plan{RunID: id(t, "run"), JobID: id(t, "job"), TenantID: id(t, "tenant"), WorkspaceID: id(t, "workspace"), Stages: []orchestration.StageSpec{spec}}
	claims := runtime_authority.ExecutionTicketClaims{TicketID: id(t, "ticket"), Issuer: "mindclade", TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, RunID: p.RunID, JobID: p.JobID, StageID: spec.StageID, RequestID: id(t, "request"), Attempt: 1, FencingToken: 1, ResolvedConfigDigest: config, Artifacts: runtime_authority.ArtifactGrant{WritableNamespaces: []string{"raw"}, MaximumReadBytes: 1024, MaximumWriteBytes: 1024}, Budget: spec.Budget, ExecutionClass: "cpu", NotBefore: now.Add(-time.Second), Deadline: now.Add(time.Minute), Expires: now.Add(30 * time.Second), PolicyEpoch: 1, RevocationEpoch: 1, IdempotencyKey: "idem"}
	ticket := runtime_authority.ExecutionTicket{Claims: claims}
	attempt := orchestration.StageAttempt{RunID: p.RunID, JobID: p.JobID, Spec: spec, Attempt: 1, FencingToken: 1, ExecutionTicketID: claims.TicketID}
	workload, err := p.CompileWorkload(id(t, "workload"), attempt, ticket, nil, "cpu-highmem", now)
	if err != nil {
		t.Fatal(err)
	}
	if workload.Operation != "fetch" {
		t.Fatal("wrong workload")
	}
}
