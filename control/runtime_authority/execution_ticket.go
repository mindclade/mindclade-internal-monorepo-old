// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package runtime_authority

import (
	"go.mindclade.dev/libs/go/identifiers"
	"time"
)

// ExecutionBudget is the global policy envelope that the Rust node runtime
// must refine with local admission before allocating concrete resources.
type ExecutionBudget struct {
	CPUMillis              uint32
	ResidentMemoryBytes    uint64
	PinnedMemoryBytes      uint64
	SharedMemoryBytes      uint64
	LocalDiskBytes         uint64
	OpenFileDescriptors    uint32
	ObjectStoreRequests    uint32
	QueuedOperations       uint32
	ChildProcesses         uint32
	CPUWorkerThreads       uint32
	GPUMemoryEstimateBytes uint64
	CheckpointStagingBytes uint64
	TelemetrySpoolBytes    uint64
	MaximumOutputBytes     uint64
}

func (b ExecutionBudget) Validate() error {
	if b.CPUMillis == 0 {
		return invalid("execution_budget_cpu_required", "execution budget requires cpu capacity", nil)
	}
	if b.ResidentMemoryBytes == 0 {
		return invalid("execution_budget_memory_required", "execution budget requires resident memory", nil)
	}
	if b.OpenFileDescriptors == 0 || b.CPUWorkerThreads == 0 {
		return invalid("execution_budget_process_limits_required", "execution budget requires fd and worker-thread limits", nil)
	}
	return nil
}
func (b ExecutionBudget) canonicalBytes() ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	e := newCanonicalEncoder("execution-budget")
	e.u32("cpu_millis", b.CPUMillis)
	e.u64("resident_memory_bytes", b.ResidentMemoryBytes)
	e.u64("pinned_memory_bytes", b.PinnedMemoryBytes)
	e.u64("shared_memory_bytes", b.SharedMemoryBytes)
	e.u64("local_disk_bytes", b.LocalDiskBytes)
	e.u32("open_file_descriptors", b.OpenFileDescriptors)
	e.u32("object_store_requests", b.ObjectStoreRequests)
	e.u32("queued_operations", b.QueuedOperations)
	e.u32("child_processes", b.ChildProcesses)
	e.u32("cpu_worker_threads", b.CPUWorkerThreads)
	e.u64("gpu_memory_estimate_bytes", b.GPUMemoryEstimateBytes)
	e.u64("checkpoint_staging_bytes", b.CheckpointStagingBytes)
	e.u64("telemetry_spool_bytes", b.TelemetrySpoolBytes)
	e.u64("maximum_output_bytes", b.MaximumOutputBytes)
	return e.bytes()
}

// ExecutionTicketClaims is the signed authorization for one durable attempt.
// Inputs and outputs remain immutable content-addressed artifacts; the ticket
// carries permission and resource bounds, not model/scientific semantics.
type ExecutionTicketClaims struct {
	TicketID                string
	Issuer                  string
	TenantID                string
	WorkspaceID             string
	RunID                   string
	JobID                   string
	StageID                 string
	RequestID               string
	Attempt                 uint32
	FencingToken            uint64
	ModelBundleDigest       identifiers.Digest
	EngineBundleDigest      identifiers.Digest
	ResolvedConfigDigest    identifiers.Digest
	ReferenceSnapshotDigest identifiers.Digest
	Artifacts               ArtifactGrant
	Budget                  ExecutionBudget
	ExecutionClass          string
	AcceleratorCapability   string
	NotBefore               time.Time
	Deadline                time.Time
	Expires                 time.Time
	PolicyEpoch             uint64
	RouteSnapshotVersion    uint64
	RevocationEpoch         uint64
	IdempotencyKey          string
}

func (c ExecutionTicketClaims) CanonicalBytes() ([]byte, error) {
	if err := c.ValidateStatic(); err != nil {
		return nil, err
	}
	e := newCanonicalEncoder("execution-ticket-claims")
	e.text("ticket_id", c.TicketID)
	e.text("issuer", c.Issuer)
	e.text("tenant_id", c.TenantID)
	e.text("workspace_id", c.WorkspaceID)
	e.text("run_id", c.RunID)
	e.text("job_id", c.JobID)
	e.text("stage_id", c.StageID)
	e.text("request_id", c.RequestID)
	e.u32("attempt", c.Attempt)
	e.u64("fencing_token", c.FencingToken)
	e.text("model_bundle_digest", c.ModelBundleDigest.String())
	e.text("engine_bundle_digest", c.EngineBundleDigest.String())
	e.text("resolved_config_digest", c.ResolvedConfigDigest.String())
	e.text("reference_snapshot_digest", c.ReferenceSnapshotDigest.String())
	artifactGrant, err := c.Artifacts.canonicalBytes()
	if err != nil {
		return nil, err
	}
	budget, err := c.Budget.canonicalBytes()
	if err != nil {
		return nil, err
	}
	e.nested("artifact_grant", artifactGrant)
	e.nested("budget", budget)
	e.text("execution_class", c.ExecutionClass)
	e.text("accelerator_capability", c.AcceleratorCapability)
	notBefore, err := unixMillis(c.NotBefore, "not_before")
	if err != nil {
		return nil, err
	}
	deadline, err := unixMillis(c.Deadline, "deadline")
	if err != nil {
		return nil, err
	}
	expires, err := unixMillis(c.Expires, "expires")
	if err != nil {
		return nil, err
	}
	e.u64("not_before_unix_millis", notBefore)
	e.u64("deadline_unix_millis", deadline)
	e.u64("expires_unix_millis", expires)
	e.u64("policy_epoch", c.PolicyEpoch)
	e.u64("route_snapshot_version", c.RouteSnapshotVersion)
	e.u64("revocation_epoch", c.RevocationEpoch)
	e.text("idempotency_key", c.IdempotencyKey)
	return e.bytes()
}
func (c ExecutionTicketClaims) ValidateStatic() error {
	if err := validateCanonicalID(c.TicketID, "ticket"); err != nil {
		return err
	}
	if c.Issuer == "" {
		return invalid("ticket_issuer_required", "ticket issuer is required", nil)
	}
	if err := validateCanonicalID(c.TenantID, "tenant"); err != nil {
		return err
	}
	if err := validateCanonicalID(c.WorkspaceID, "workspace"); err != nil {
		return err
	}
	for name, value := range map[string]string{"run": c.RunID, "job": c.JobID, "stage": c.StageID, "request": c.RequestID} {
		if value != "" {
			if _, err := identifiers.ParseID(value); err != nil {
				return invalid("invalid_"+name+"_id", name+" id must be a canonical Mindclade identifier", err)
			}
		}
	}
	if c.Attempt == 0 || c.FencingToken == 0 {
		return invalid("ticket_attempt_fencing_required", "ticket attempt and fencing token must be non-zero", nil)
	}
	if !c.ResolvedConfigDigest.Valid() {
		return invalid("ticket_config_digest_required", "resolved configuration digest is required", nil)
	}
	if c.ExecutionClass == "" {
		return invalid("execution_class_required", "execution class is required", nil)
	}
	if c.NotBefore.IsZero() || c.Deadline.IsZero() || c.Expires.IsZero() || !c.Deadline.After(c.NotBefore) || !c.Expires.After(c.NotBefore) || c.Expires.After(c.Deadline) {
		return invalid("ticket_time_window_invalid", "ticket time window is invalid", nil)
	}
	if c.PolicyEpoch == 0 || c.RevocationEpoch == 0 {
		return invalid("ticket_policy_epoch_required", "policy and revocation epochs must be non-zero", nil)
	}
	if err := c.Artifacts.Validate(); err != nil {
		return err
	}
	return c.Budget.Validate()
}
