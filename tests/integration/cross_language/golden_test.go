// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package cross_language

import (
	"encoding/json"
	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/control/registry/models"
	ra "go.mindclade.dev/control/runtime_authority"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// fixturePackage is the workspace-relative directory holding the golden vectors.
const fixturePackage = "tests/integration/cross_language/fixtures"

// fixture reads a golden vector under both `go test` and Bazel.
//
// Under `go test` the source path from runtime.Caller is absolute and resolves
// directly. Under Bazel it is workspace-relative and the working directory is
// not the runfiles root, so the vectors have to be found through the runfiles
// tree that the target's `data` attribute populates. Reading only the caller
// path is why this test could not pass under Bazel at all.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	candidates := []string{filepath.Join(filepath.Dir(file), "fixtures", name)}
	workspace := os.Getenv("TEST_WORKSPACE")
	for _, root := range []string{os.Getenv("TEST_SRCDIR"), os.Getenv("RUNFILES_DIR")} {
		if root == "" {
			continue
		}
		if workspace != "" {
			candidates = append(candidates, filepath.Join(root, workspace, fixturePackage, name))
		}
		candidates = append(candidates, filepath.Join(root, fixturePackage, name))
	}
	for _, candidate := range candidates {
		b, err := os.ReadFile(candidate)
		if err == nil {
			return b
		}
	}
	t.Fatalf("golden vector %s not found in any of %v", name, candidates)
	return nil
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
func TestModelDescriptorGoldenRemainsStable(t *testing.T) {
	created := time.UnixMilli(1800000000000).UTC()
	descriptor := models.Descriptor{
		ModelID:              "model_019c0000000070008000000000000001",
		Family:               "novafold",
		Version:              "3.1.0",
		Lifecycle:            models.LifecycleServing,
		ModelBundleDigest:    identifiers.SHA256String("model-bundle"),
		EngineBundleDigest:   identifiers.SHA256String("engine-bundle"),
		ResolvedConfigDigest: identifiers.SHA256String("resolved-config"),
		KernelManifestDigest: identifiers.SHA256String("kernel-manifest"),
		SafetyPolicyDigest:   identifiers.SHA256String("safety-policy"),
		Capabilities:         []string{"msa", "structure", "templates"},
		CompatibilityClasses: []models.CompatibilityClass{
			{ClassID: "forward-bf16-small", ExecutionKind: models.ExecutionForward, Precision: models.PrecisionBF16, ShapeBucket: "tokens<=1024", MaximumBatchRequests: 8, MaximumBatchGPUBytes: 8 << 30, MaximumInputUnits: 1024, MaximumOutputUnits: 512},
			{ClassID: "diffusion-fp16-large", ExecutionKind: models.ExecutionDiffusionSample, Precision: models.PrecisionFP16, ShapeBucket: "atoms<=8192", MaximumBatchRequests: 2, MaximumBatchGPUBytes: 32 << 30, MaximumInputUnits: 8192, MaximumOutputUnits: 4096},
		},
		Envelope: models.ResourceEnvelope{
			WeightsResidentBytes:      24 << 30,
			HostMemoryBytes:           32 << 30,
			GPUMemoryFloorBytes:       40 << 30,
			GPUMemoryPerRequestBytes:  2 << 30,
			MaximumConcurrentRequests: 4,
			LoadDeadline:              2 * time.Minute,
			DrainDeadline:             30 * time.Second,
		},
		AcceleratorCapability: "sm90",
		MinimumRuntimeVersion: "1.4.0",
		SchemaVersion:         1,
		PolicyEpoch:           12,
		Created:               created,
		Expires:               created.Add(24 * time.Hour),
	}
	payload, err := descriptor.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != string(fixture(t, "model_descriptor_v1.bin")) {
		t.Fatal("inference-model-descriptor/v1 golden changed")
	}
	if err = descriptor.SealDigest(); err != nil {
		t.Fatal(err)
	}
	var meta struct {
		DescriptorDigest string `json:"descriptor_digest"`
	}
	if err = json.Unmarshal(fixture(t, "model_descriptor_v1.json"), &meta); err != nil {
		t.Fatal(err)
	}
	if descriptor.DescriptorDigest.String() != meta.DescriptorDigest {
		t.Fatalf("sealed digest = %s", descriptor.DescriptorDigest)
	}
	if err = descriptor.VerifyDigest(); err != nil {
		t.Fatal(err)
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
