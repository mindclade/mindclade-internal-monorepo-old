// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package models is the durable catalog authority for servable model
// descriptors published to the online inference path.
//
// The package owns the writer side of mindclade.inference.v1.ModelDescriptor:
// it validates a descriptor, seals its canonical digest, and applies the
// publication policy that decides which lifecycle may serve traffic. It does
// not route requests, form batches, reserve accelerators, or interpret model
// numerics; those belong to control/routing, the Rust data plane, and Python
// respectively.
package models

import (
	"time"

	"go.mindclade.dev/libs/go/identifiers"
)

// Lifecycle is the publication state of a servable model. The values mirror
// mindclade.inference.v1.ModelLifecycle and are the wire text used by the
// canonical descriptor encoding.
type Lifecycle string

const (
	LifecycleDraft      Lifecycle = "draft"
	LifecycleQualified  Lifecycle = "qualified"
	LifecycleServing    Lifecycle = "serving"
	LifecycleDeprecated Lifecycle = "deprecated"
	LifecycleRevoked    Lifecycle = "revoked"
)

// Valid reports whether the lifecycle is a declared state.
func (l Lifecycle) Valid() bool {
	switch l {
	case LifecycleDraft, LifecycleQualified, LifecycleServing, LifecycleDeprecated, LifecycleRevoked:
		return true
	}
	return false
}

// ExecutionKind is the coarse execution shape a compatibility class covers.
type ExecutionKind string

const (
	ExecutionForward         ExecutionKind = "forward"
	ExecutionDiffusionSample ExecutionKind = "diffusion_sample"
	ExecutionEmbedding       ExecutionKind = "embedding"
	ExecutionScoring         ExecutionKind = "scoring"
)

// Valid reports whether the execution kind is declared.
func (k ExecutionKind) Valid() bool {
	switch k {
	case ExecutionForward, ExecutionDiffusionSample, ExecutionEmbedding, ExecutionScoring:
		return true
	}
	return false
}

// Precision is the declared numerical precision of a compatibility class.
type Precision string

const (
	PrecisionFP32 Precision = "fp32"
	PrecisionTF32 Precision = "tf32"
	PrecisionBF16 Precision = "bf16"
	PrecisionFP16 Precision = "fp16"
	PrecisionFP8  Precision = "fp8"
)

// Valid reports whether the precision is declared.
func (p Precision) Valid() bool {
	switch p {
	case PrecisionFP32, PrecisionTF32, PrecisionBF16, PrecisionFP16, PrecisionFP8:
		return true
	}
	return false
}

// CompatibilityClass is a coarse batching class the data plane admits against.
// Python remains authoritative for which admitted requests share tensors.
type CompatibilityClass struct {
	ClassID              string        `json:"class_id"`
	ExecutionKind        ExecutionKind `json:"execution_kind"`
	Precision            Precision     `json:"precision"`
	ShapeBucket          string        `json:"shape_bucket"`
	MaximumBatchRequests uint32        `json:"maximum_batch_requests"`
	MaximumBatchGPUBytes uint64        `json:"maximum_batch_gpu_bytes"`
	MaximumInputUnits    uint64        `json:"maximum_input_units"`
	MaximumOutputUnits   uint64        `json:"maximum_output_units"`
}

// ResourceEnvelope is the reservation the runtime host makes before a worker
// forms a batch. Values are declared floors and per-request increments.
type ResourceEnvelope struct {
	WeightsResidentBytes      uint64        `json:"weights_resident_bytes"`
	HostMemoryBytes           uint64        `json:"host_memory_bytes"`
	GPUMemoryFloorBytes       uint64        `json:"gpu_memory_floor_bytes"`
	GPUMemoryPerRequestBytes  uint64        `json:"gpu_memory_per_request_bytes"`
	MaximumConcurrentRequests uint32        `json:"maximum_concurrent_requests"`
	LoadDeadline              time.Duration `json:"load_deadline_nanoseconds"`
	DrainDeadline             time.Duration `json:"drain_deadline_nanoseconds"`
}

// Descriptor is durable catalog state for one servable model.
//
// DescriptorDigest is sealed by SealDigest over the canonical encoding of every
// other field. A caller never sets it directly; Publish rejects a descriptor
// whose digest does not match its content.
type Descriptor struct {
	DescriptorDigest      identifiers.Digest   `json:"descriptor_digest,omitempty"`
	ModelID               string               `json:"model_id"`
	Family                string               `json:"family"`
	Version               string               `json:"version"`
	Lifecycle             Lifecycle            `json:"lifecycle"`
	ModelBundleDigest     identifiers.Digest   `json:"model_bundle_digest"`
	EngineBundleDigest    identifiers.Digest   `json:"engine_bundle_digest"`
	ResolvedConfigDigest  identifiers.Digest   `json:"resolved_config_digest"`
	KernelManifestDigest  identifiers.Digest   `json:"kernel_manifest_digest"`
	SafetyPolicyDigest    identifiers.Digest   `json:"safety_policy_digest"`
	Capabilities          []string             `json:"capabilities"`
	CompatibilityClasses  []CompatibilityClass `json:"compatibility_classes"`
	Envelope              ResourceEnvelope     `json:"envelope"`
	AcceleratorCapability string               `json:"accelerator_capability"`
	MinimumRuntimeVersion string               `json:"minimum_runtime_version"`
	SchemaVersion         uint32               `json:"schema_version"`
	PolicyEpoch           uint64               `json:"policy_epoch"`
	Created               time.Time            `json:"created"`
	Expires               time.Time            `json:"expires"`
}
