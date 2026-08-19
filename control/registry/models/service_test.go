// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package models

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/identifiers"
)

var publishedAt = time.UnixMilli(1800000000000).UTC()

type memoryRepository struct {
	stored map[string]Descriptor
	tamper func(Descriptor) Descriptor
}

func newRepository() *memoryRepository {
	return &memoryRepository{stored: map[string]Descriptor{}}
}

func (r *memoryRepository) PutDescriptor(_ context.Context, d Descriptor) error {
	r.stored[d.DescriptorDigest.String()] = d
	return nil
}

func (r *memoryRepository) GetDescriptor(_ context.Context, digest identifiers.Digest) (Descriptor, error) {
	d, ok := r.stored[digest.String()]
	if !ok {
		return Descriptor{}, invalid("model_descriptor_not_found", "model descriptor was not found", nil)
	}
	if r.tamper != nil {
		return r.tamper(d), nil
	}
	return d, nil
}

func digestOf(t *testing.T, seed string) identifiers.Digest {
	t.Helper()
	return identifiers.SHA256String(seed)
}

func validDescriptor(t *testing.T) Descriptor {
	t.Helper()
	return Descriptor{
		ModelID:              "model_019c0000000070008000000000000001",
		Family:               "novafold",
		Version:              "3.1.0",
		Lifecycle:            LifecycleServing,
		ModelBundleDigest:    digestOf(t, "model-bundle"),
		EngineBundleDigest:   digestOf(t, "engine-bundle"),
		ResolvedConfigDigest: digestOf(t, "resolved-config"),
		KernelManifestDigest: digestOf(t, "kernel-manifest"),
		SafetyPolicyDigest:   digestOf(t, "safety-policy"),
		Capabilities:         []string{"msa", "structure", "templates"},
		CompatibilityClasses: []CompatibilityClass{
			{
				ClassID:              "forward-bf16-small",
				ExecutionKind:        ExecutionForward,
				Precision:            PrecisionBF16,
				ShapeBucket:          "tokens<=1024",
				MaximumBatchRequests: 8,
				MaximumBatchGPUBytes: 8 << 30,
				MaximumInputUnits:    1024,
				MaximumOutputUnits:   512,
			},
			{
				ClassID:              "diffusion-fp16-large",
				ExecutionKind:        ExecutionDiffusionSample,
				Precision:            PrecisionFP16,
				ShapeBucket:          "atoms<=8192",
				MaximumBatchRequests: 2,
				MaximumBatchGPUBytes: 32 << 30,
				MaximumInputUnits:    8192,
				MaximumOutputUnits:   4096,
			},
		},
		Envelope: ResourceEnvelope{
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
		Created:               publishedAt,
		Expires:               publishedAt.Add(24 * time.Hour),
	}
}

func newService(repository Repository) Service {
	return Service{
		Repository: repository,
		Policy:     ProductionPolicy(),
		Clock:      clock.NewFake(publishedAt),
	}
}

func TestPublishSealsAndResolveVerifiesTheDigest(t *testing.T) {
	repository := newRepository()
	service := newService(repository)
	published, err := service.Publish(context.Background(), validDescriptor(t))
	if err != nil {
		t.Fatal(err)
	}
	if !published.DescriptorDigest.Valid() {
		t.Fatal("publish did not seal the descriptor digest")
	}
	resolved, err := service.Resolve(context.Background(), published.DescriptorDigest)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.DescriptorDigest.Equal(published.DescriptorDigest) {
		t.Fatal("resolve returned a different descriptor")
	}
	if !service.Servable(resolved) {
		t.Fatal("a serving descriptor inside its validity window must be servable")
	}
}

func TestCanonicalBytesAreStableAndOrderIndependent(t *testing.T) {
	first, err := validDescriptor(t).CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	reordered := validDescriptor(t)
	reordered.CompatibilityClasses = []CompatibilityClass{
		reordered.CompatibilityClasses[1],
		reordered.CompatibilityClasses[0],
	}
	second, err := reordered.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("canonical bytes must not depend on declared class order")
	}
	if !strings.HasPrefix(string(first), canonicalDocumentType+"\n") {
		t.Fatal("canonical bytes must open with the document type")
	}
}

func TestResolveRejectsATamperedDescriptor(t *testing.T) {
	repository := newRepository()
	service := newService(repository)
	published, err := service.Publish(context.Background(), validDescriptor(t))
	if err != nil {
		t.Fatal(err)
	}
	repository.tamper = func(d Descriptor) Descriptor {
		d.Version = "3.1.1"
		return d
	}
	if _, err = service.Resolve(context.Background(), published.DescriptorDigest); err == nil {
		t.Fatal("expected a tampered descriptor to fail digest verification")
	}
}

func TestPublishRejectsAMissingMandatoryCapability(t *testing.T) {
	descriptor := validDescriptor(t)
	descriptor.Capabilities = []string{"msa", "templates"}
	if _, err := newService(newRepository()).Publish(context.Background(), descriptor); err == nil {
		t.Fatal("expected the missing structure capability to be rejected")
	}
}

func TestPublishRejectsAServingDescriptorThatExpiresTooSoon(t *testing.T) {
	descriptor := validDescriptor(t)
	descriptor.Expires = publishedAt.Add(time.Minute)
	if _, err := newService(newRepository()).Publish(context.Background(), descriptor); err == nil {
		t.Fatal("expected the short validity window to be rejected")
	}
}

func TestValidationRejectsNoncanonicalCapabilitiesAndDelimiters(t *testing.T) {
	unsorted := validDescriptor(t)
	unsorted.Capabilities = []string{"structure", "msa"}
	if err := unsorted.Validate(); err == nil {
		t.Fatal("expected unsorted capabilities to be rejected")
	}
	delimited := validDescriptor(t)
	delimited.Capabilities = []string{"structure|smuggled"}
	if err := delimited.Validate(); err == nil {
		t.Fatal("expected a reserved delimiter to be rejected")
	}
}

func TestValidationRejectsDuplicateCompatibilityClasses(t *testing.T) {
	descriptor := validDescriptor(t)
	descriptor.CompatibilityClasses = []CompatibilityClass{
		descriptor.CompatibilityClasses[0],
		descriptor.CompatibilityClasses[0],
	}
	if err := descriptor.Validate(); err == nil {
		t.Fatal("expected duplicate class ids to be rejected")
	}
}

func TestVerifyDigestRejectsAnUnsealedDescriptor(t *testing.T) {
	if err := validDescriptor(t).VerifyDigest(); err == nil {
		t.Fatal("expected an unsealed descriptor to be rejected")
	}
}

func TestPublishRequiresConfiguredProviders(t *testing.T) {
	if _, err := (Service{Policy: ProductionPolicy()}).Publish(context.Background(), validDescriptor(t)); err == nil {
		t.Fatal("expected a missing repository to be rejected")
	}
	if _, err := (Service{Repository: newRepository(), Policy: ProductionPolicy()}).Publish(context.Background(), validDescriptor(t)); err == nil {
		t.Fatal("expected a missing clock to be rejected")
	}
}
