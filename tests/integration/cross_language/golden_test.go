// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package cross_language

import (
	"encoding/json"
	"go.mindclade.dev/control/artifacts"
	ra "go.mindclade.dev/control/runtime_authority"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(file), "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func d(t *testing.T, s string) identifiers.Digest {
	t.Helper()
	v, err := identifiers.ParseDigest(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestExecutionTicketGoldenRemainsStable(t *testing.T) {
	now := time.UnixMilli(1800000000000).UTC()
	c := ra.ExecutionTicketClaims{TicketID: "ticket_019c0000000070008000000000000001", Issuer: "control", TenantID: "tenant_019c0000000070008000000000000002", WorkspaceID: "workspace_019c0000000070008000000000000003", StageID: "stage_019c0000000070008000000000000004", Attempt: 1, FencingToken: 9, ModelBundleDigest: d(t, "sha256:"+repeat("1", 64)), EngineBundleDigest: d(t, "sha256:"+repeat("2", 64)), ResolvedConfigDigest: d(t, "sha256:"+repeat("3", 64)), ReferenceSnapshotDigest: d(t, "sha256:"+repeat("4", 64)), Artifacts: ra.ArtifactGrant{ReadableDigests: []string{"sha256:" + repeat("a", 64)}, WritableNamespaces: []string{"tenant/t1/run/r1"}, MaximumReadBytes: 1024, MaximumWriteBytes: 2048, AllowRangeReads: true, AllowMultipartWrites: true}, Budget: ra.ExecutionBudget{CPUMillis: 2000, ResidentMemoryBytes: 8 << 30, PinnedMemoryBytes: 1 << 30, SharedMemoryBytes: 512 << 20, LocalDiskBytes: 16 << 30, OpenFileDescriptors: 128, ObjectStoreRequests: 16, QueuedOperations: 8, ChildProcesses: 2, CPUWorkerThreads: 8, GPUMemoryEstimateBytes: 40 << 30, CheckpointStagingBytes: 4 << 30, TelemetrySpoolBytes: 64 << 20, MaximumOutputBytes: 2 << 30}, ExecutionClass: "gpu", AcceleratorCapability: "sm90", NotBefore: now, Deadline: now.Add(10 * time.Minute), Expires: now.Add(5 * time.Minute), PolicyEpoch: 12, RouteSnapshotVersion: 34, RevocationEpoch: 7, IdempotencyKey: "run:r1:stage:s1:attempt:1"}
	b, err := c.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(fixture(t, "execution_ticket_claims_v1.bin")) {
		t.Fatal("MCCE1 golden changed")
	}
}
func TestResourceVersionCrossLanguageFormat(t *testing.T) {
	v, err := resourceversion.New(42, d(t, "sha256:"+repeat("a", 64)))
	if err != nil {
		t.Fatal(err)
	}
	if v.String() != "rv1:42:sha256:"+repeat("a", 64) {
		t.Fatal(v.String())
	}
}

type primitiveFixture struct {
	ResourceID      string `json:"resource_id"`
	ResourceIDKind  string `json:"resource_id_kind"`
	Digest          string `json:"digest"`
	ResourceVersion string `json:"resource_version"`
	ArtifactRef     struct {
		Digest        string `json:"digest"`
		SizeBytes     uint64 `json:"size_bytes"`
		MediaType     string `json:"media_type"`
		LogicalKind   string `json:"logical_kind"`
		SchemaVersion uint32 `json:"schema_version"`
	} `json:"artifact_ref"`
}

func TestPrimitiveGoldenParsesThroughGoContracts(t *testing.T) {
	var f primitiveFixture
	if err := json.Unmarshal(fixture(t, "primitives_v1.json"), &f); err != nil {
		t.Fatal(err)
	}
	id, err := identifiers.ParseID(f.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	if id.Kind().String() != f.ResourceIDKind {
		t.Fatalf("kind mismatch: %s", id.Kind())
	}
	digest, err := identifiers.ParseDigest(f.Digest)
	if err != nil {
		t.Fatal(err)
	}
	version, err := resourceversion.Parse(f.ResourceVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !version.Digest().Equal(digest) || version.Generation() != 42 {
		t.Fatal("resource version fixture mismatch")
	}
	ref := artifacts.Ref{Digest: d(t, f.ArtifactRef.Digest), SizeBytes: f.ArtifactRef.SizeBytes, MediaType: f.ArtifactRef.MediaType, LogicalKind: f.ArtifactRef.LogicalKind, SchemaVersion: f.ArtifactRef.SchemaVersion}
	if err := ref.Validate(); err != nil {
		t.Fatal(err)
	}
}

func repeat(s string, n int) string {
	b := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		b = append(b, s...)
	}
	return string(b)
}
