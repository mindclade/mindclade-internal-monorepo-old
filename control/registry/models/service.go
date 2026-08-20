// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package models

import (
	"context"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/identifiers"
)

// Repository is the durable store for published model descriptors. It is the
// only seam this package needs; provider construction belongs to the service
// that wires the control plane.
type Repository interface {
	PutDescriptor(context.Context, Descriptor) error
	GetDescriptor(context.Context, identifiers.Digest) (Descriptor, error)
}

// Service publishes and resolves servable model descriptors.
//
// Failure behaviour: every method returns an invalid-argument fault with a
// stable reason and no retry policy when the descriptor or its digest is not
// acceptable. Repository errors propagate unchanged so the caller's transport
// layer decides retryability. No method mutates a descriptor the caller owns
// except Publish, which seals the digest on its own copy.
type Service struct {
	Repository Repository
	Policy     PublicationPolicy
	Clock      clock.Clock
}

func (s Service) now() time.Time {
	if s.Clock == nil {
		return time.Time{}
	}
	return s.Clock.Now().UTC()
}

// Publish validates a descriptor, applies the publication policy, seals the
// canonical digest, and stores the result. The sealed descriptor is returned so
// the caller can reference the digest it must quote downstream.
func (s Service) Publish(ctx context.Context, d Descriptor) (Descriptor, error) {
	if s.Repository == nil {
		return Descriptor{}, invalid("model_repository_unavailable", "model descriptor repository is unavailable", nil)
	}
	if s.Clock == nil {
		return Descriptor{}, invalid("model_clock_unavailable", "model registry clock is unavailable", nil)
	}
	now := s.now()
	if err := s.Policy.Evaluate(d, now); err != nil {
		return Descriptor{}, err
	}
	if err := d.SealDigest(); err != nil {
		return Descriptor{}, err
	}
	if err := s.Repository.PutDescriptor(ctx, d); err != nil {
		return Descriptor{}, err
	}
	return d, nil
}

// Resolve loads a descriptor by its sealed digest and re-verifies the seal
// before returning it. A store that hands back mutated content fails here
// rather than reaching the data plane.
func (s Service) Resolve(ctx context.Context, digest identifiers.Digest) (Descriptor, error) {
	if s.Repository == nil {
		return Descriptor{}, invalid("model_repository_unavailable", "model descriptor repository is unavailable", nil)
	}
	if !digest.Valid() {
		return Descriptor{}, invalid("model_descriptor_digest_invalid", "model descriptor digest is invalid", nil)
	}
	d, err := s.Repository.GetDescriptor(ctx, digest)
	if err != nil {
		return Descriptor{}, err
	}
	if err := d.VerifyDigest(); err != nil {
		return Descriptor{}, err
	}
	if !d.DescriptorDigest.Equal(digest) {
		return Descriptor{}, invalid("model_descriptor_identity_mismatch", "model descriptor store returned a different descriptor", nil)
	}
	return d, nil
}

// Servable reports whether a descriptor may take online traffic at time now.
// The data plane makes the same decision locally from the descriptor it holds;
// this is the control-plane view used when publishing routes.
func (s Service) Servable(d Descriptor) bool {
	if d.Lifecycle != LifecycleServing {
		return false
	}
	if s.Clock == nil {
		return false
	}
	return s.now().Before(d.Expires)
}
