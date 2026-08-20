// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package models

import (
	"sort"
	"strconv"
	"strings"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

// Bounds are declared rather than inferred so a descriptor can never grow large
// enough to make canonical encoding or data-plane admission unbounded work.
const (
	MaximumCapabilities        = 256
	MaximumCompatibilityClass  = 64
	MaximumTextBytes           = 4096
	canonicalDocumentType      = "inference-model-descriptor/v1"
	descriptorSchemaVersionMin = 1
)

func invalid(reason, message string, cause error) error {
	if cause == nil {
		return faults.New(faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.registry.models"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.registry.models"), faults.WithRetryPolicy(faults.NoRetry()))
}

// canonicalText rejects the two delimiters the canonical encoding reserves.
// Without this the encoding would not be injective and two different
// descriptors could seal to the same digest.
func canonicalText(value string) bool {
	return value != "" &&
		len(value) <= MaximumTextBytes &&
		!strings.ContainsAny(value, "\n|")
}

// Validate reports whether the descriptor is internally consistent and within
// declared bounds. It does not check the sealed digest; see VerifyDigest.
func (d Descriptor) Validate() error {
	if _, err := identifiers.ParseID(d.ModelID); err != nil {
		return invalid("model_id_invalid", "model id must be canonical", err)
	}
	if !canonicalText(d.Family) || !canonicalText(d.Version) {
		return invalid("model_identity_invalid", "model family and version are required", nil)
	}
	if !canonicalText(d.AcceleratorCapability) || !canonicalText(d.MinimumRuntimeVersion) {
		return invalid("model_runtime_requirements_invalid", "accelerator capability and minimum runtime version are required", nil)
	}
	if !d.Lifecycle.Valid() {
		return invalid("model_lifecycle_invalid", "model lifecycle is not a declared state", nil)
	}
	for _, digest := range []identifiers.Digest{
		d.ModelBundleDigest,
		d.EngineBundleDigest,
		d.ResolvedConfigDigest,
		d.KernelManifestDigest,
		d.SafetyPolicyDigest,
	} {
		if !digest.Valid() {
			return invalid("model_bundle_digest_invalid", "model, engine, config, kernel, and safety digests are required", nil)
		}
	}
	if d.SchemaVersion < descriptorSchemaVersionMin {
		return invalid("model_schema_version_invalid", "model descriptor schema version must be positive", nil)
	}
	if d.PolicyEpoch == 0 {
		return invalid("model_policy_epoch_invalid", "model descriptor policy epoch must be positive", nil)
	}
	if err := d.validateCapabilities(); err != nil {
		return err
	}
	if err := d.validateClasses(); err != nil {
		return err
	}
	if err := d.Envelope.Validate(); err != nil {
		return err
	}
	if d.Created.IsZero() || !d.Expires.After(d.Created) {
		return invalid("model_validity_window_invalid", "model descriptor must expire after it was created", nil)
	}
	return nil
}

func (d Descriptor) validateCapabilities() error {
	if len(d.Capabilities) > MaximumCapabilities {
		return invalid("model_capability_count_exceeded", "model capability count exceeds bound", nil)
	}
	for index, capability := range d.Capabilities {
		if !canonicalText(capability) {
			return invalid("model_capability_invalid", "model capability is empty or contains a reserved delimiter", nil)
		}
		if index > 0 && d.Capabilities[index-1] >= capability {
			return invalid("model_capabilities_noncanonical", "model capabilities must be strictly sorted and unique", nil)
		}
	}
	return nil
}

func (d Descriptor) validateClasses() error {
	if len(d.CompatibilityClasses) == 0 || len(d.CompatibilityClasses) > MaximumCompatibilityClass {
		return invalid("model_compatibility_class_count_invalid", "model must declare between one and 64 compatibility classes", nil)
	}
	seen := make(map[string]struct{}, len(d.CompatibilityClasses))
	for _, class := range d.CompatibilityClasses {
		if err := class.Validate(); err != nil {
			return err
		}
		if _, duplicate := seen[class.ClassID]; duplicate {
			return invalid("model_compatibility_class_duplicate", "compatibility class ids must be unique", nil)
		}
		seen[class.ClassID] = struct{}{}
	}
	return nil
}

// Validate reports whether one declared compatibility class is usable for
// admission and reservation.
func (c CompatibilityClass) Validate() error {
	if !canonicalText(c.ClassID) || !canonicalText(c.ShapeBucket) {
		return invalid("compatibility_class_identity_invalid", "compatibility class id and shape bucket are required", nil)
	}
	if !c.ExecutionKind.Valid() || !c.Precision.Valid() {
		return invalid("compatibility_class_execution_invalid", "compatibility class execution kind and precision must be declared", nil)
	}
	if c.MaximumBatchRequests == 0 || c.MaximumBatchGPUBytes == 0 {
		return invalid("compatibility_class_batch_bounds_invalid", "compatibility class batch bounds must be positive", nil)
	}
	if c.MaximumInputUnits == 0 || c.MaximumOutputUnits == 0 {
		return invalid("compatibility_class_unit_bounds_invalid", "compatibility class unit bounds must be positive", nil)
	}
	return nil
}

// Validate reports whether the reservation envelope is usable by the runtime
// host. Every field is a hard reservation input, so none of them may be zero.
func (e ResourceEnvelope) Validate() error {
	if e.WeightsResidentBytes == 0 || e.HostMemoryBytes == 0 {
		return invalid("model_envelope_memory_invalid", "model envelope host and weight memory must be positive", nil)
	}
	if e.GPUMemoryFloorBytes == 0 || e.GPUMemoryPerRequestBytes == 0 {
		return invalid("model_envelope_accelerator_invalid", "model envelope accelerator memory must be positive", nil)
	}
	if e.MaximumConcurrentRequests == 0 {
		return invalid("model_envelope_concurrency_invalid", "model envelope concurrency must be positive", nil)
	}
	if e.LoadDeadline <= 0 || e.DrainDeadline <= 0 {
		return invalid("model_envelope_deadline_invalid", "model envelope load and drain deadlines must be positive", nil)
	}
	return nil
}

// CanonicalBytes returns the newline-framed `inference-model-descriptor/v1`
// document that the sealed digest is computed over.
//
// The format is deliberately reproducible without a protobuf or JSON library so
// that Go (the writer) and Python (the worker-side verifier) can agree byte for
// byte. Repeated fields are emitted in sorted order and every text field is
// validated to exclude the newline and vertical-bar delimiters, which makes the
// encoding injective.
func (d Descriptor) CanonicalBytes() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	var out strings.Builder
	out.WriteString(canonicalDocumentType)
	out.WriteByte('\n')
	for _, line := range []string{
		d.ModelID,
		d.Family,
		d.Version,
		string(d.Lifecycle),
		d.ModelBundleDigest.String(),
		d.EngineBundleDigest.String(),
		d.ResolvedConfigDigest.String(),
		d.KernelManifestDigest.String(),
		d.SafetyPolicyDigest.String(),
		d.AcceleratorCapability,
		d.MinimumRuntimeVersion,
		strconv.FormatUint(uint64(d.SchemaVersion), 10),
		strconv.FormatUint(d.PolicyEpoch, 10),
		strconv.FormatInt(d.Created.UTC().UnixMilli(), 10),
		strconv.FormatInt(d.Expires.UTC().UnixMilli(), 10),
	} {
		out.WriteString(line)
		out.WriteByte('\n')
	}
	for _, capability := range d.Capabilities {
		out.WriteString("capability|")
		out.WriteString(capability)
		out.WriteByte('\n')
	}
	classes := append([]CompatibilityClass(nil), d.CompatibilityClasses...)
	sort.Slice(classes, func(i, j int) bool { return classes[i].ClassID < classes[j].ClassID })
	for _, class := range classes {
		out.WriteString(strings.Join([]string{
			"class",
			class.ClassID,
			string(class.ExecutionKind),
			string(class.Precision),
			class.ShapeBucket,
			strconv.FormatUint(uint64(class.MaximumBatchRequests), 10),
			strconv.FormatUint(class.MaximumBatchGPUBytes, 10),
			strconv.FormatUint(class.MaximumInputUnits, 10),
			strconv.FormatUint(class.MaximumOutputUnits, 10),
		}, "|"))
		out.WriteByte('\n')
	}
	out.WriteString(strings.Join([]string{
		"envelope",
		strconv.FormatUint(d.Envelope.WeightsResidentBytes, 10),
		strconv.FormatUint(d.Envelope.HostMemoryBytes, 10),
		strconv.FormatUint(d.Envelope.GPUMemoryFloorBytes, 10),
		strconv.FormatUint(d.Envelope.GPUMemoryPerRequestBytes, 10),
		strconv.FormatUint(uint64(d.Envelope.MaximumConcurrentRequests), 10),
		strconv.FormatInt(d.Envelope.LoadDeadline.Milliseconds(), 10),
		strconv.FormatInt(d.Envelope.DrainDeadline.Milliseconds(), 10),
	}, "|"))
	out.WriteByte('\n')
	return []byte(out.String()), nil
}

// SealDigest computes and stores the canonical descriptor digest.
func (d *Descriptor) SealDigest() error {
	payload, err := d.CanonicalBytes()
	if err != nil {
		return err
	}
	d.DescriptorDigest = identifiers.SHA256(payload)
	return nil
}

// VerifyDigest reports whether the sealed digest matches the descriptor's
// content. A descriptor that arrives without a matching digest is rejected
// rather than resealed, so a mutated descriptor can never be republished.
func (d Descriptor) VerifyDigest() error {
	payload, err := d.CanonicalBytes()
	if err != nil {
		return err
	}
	if !d.DescriptorDigest.Valid() {
		return invalid("model_descriptor_unsealed", "model descriptor digest is not sealed", nil)
	}
	if !d.DescriptorDigest.Equal(identifiers.SHA256(payload)) {
		return invalid("model_descriptor_digest_mismatch", "model descriptor digest does not match its content", nil)
	}
	return nil
}
