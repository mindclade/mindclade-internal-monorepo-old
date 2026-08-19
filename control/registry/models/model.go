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
	ClassID              string
	ExecutionKind        ExecutionKind
	Precision            Precision
	ShapeBucket          string
	MaximumBatchRequests uint32
	MaximumBatchGPUBytes uint64
	MaximumInputUnits    uint64
	MaximumOutputUnits   uint64
}

// ResourceEnvelope is the reservation the runtime host makes before a worker
// forms a batch. Values are declared floors and per-request increments.
type ResourceEnvelope struct {
	WeightsResidentBytes      uint64
	HostMemoryBytes           uint64
	GPUMemoryFloorBytes       uint64
	GPUMemoryPerRequestBytes  uint64
	MaximumConcurrentRequests uint32
	LoadDeadline              time.Duration
	DrainDeadline             time.Duration
}

// Descriptor is durable catalog state for one servable model.
//
// DescriptorDigest is sealed by SealDigest over the canonical encoding of every
// other field. A caller never sets it directly; Publish rejects a descriptor
// whose digest does not match its content.
type Descriptor struct {
	DescriptorDigest      identifiers.Digest
	ModelID               string
	Family                string
	Version               string
	Lifecycle             Lifecycle
	ModelBundleDigest     identifiers.Digest
	EngineBundleDigest    identifiers.Digest
	ResolvedConfigDigest  identifiers.Digest
	KernelManifestDigest  identifiers.Digest
	SafetyPolicyDigest    identifiers.Digest
	Capabilities          []string
	CompatibilityClasses  []CompatibilityClass
	Envelope              ResourceEnvelope
	AcceleratorCapability string
	MinimumRuntimeVersion string
	SchemaVersion         uint32
	PolicyEpoch           uint64
	Created               time.Time
	Expires               time.Time
}
