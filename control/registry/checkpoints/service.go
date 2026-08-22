// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package checkpoints reserves the boundary defined by the production blueprint.
package checkpoints

import (
	"context"
	"time"

	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/identifiers"
)

// Repository is the create-only and optimistic-concurrency storage seam for
// checkpoint records. Provider construction belongs in the control-plane
// composition root.
type Repository interface {
	Create(context.Context, Record) error
	Get(context.Context, string) (Record, error)
	CompareAndSwap(context.Context, Record, uint64) error
}

// VerifiedPublication contains facts derived by resolving and decoding the
// immutable manifest, commit marker, and component artifacts. These facts are
// deliberately separate from the caller-authored Record projection.
type VerifiedPublication struct {
	CheckpointID        string
	RunID               string
	StageAttempt        uint32
	CheckpointAttempt   uint64
	FencingToken        uint64
	Microbatches        uint64
	OptimizerStep       uint64
	Samples             uint64
	DataPosition        uint64
	Manifest            artifacts.Ref
	Commit              artifacts.Ref
	TopologyFingerprint identifiers.Digest
	ParentManifest      artifacts.Ref
}

// CheckpointVerifier resolves immutable bytes, verifies every ArtifactRef and
// canonical typed payload, and returns facts derived from those bytes. A
// connected verifier must reject unknown/non-canonical protobuf data, a commit
// that does not bind the manifest, component digest failures, and invalid
// parent lineage.
type CheckpointVerifier interface {
	VerifyCommittedCheckpoint(context.Context, Record) (VerifiedPublication, error)
}

// PublicationAuthority checks the verified stage attempt against the current
// admitted run attempt, fence, and exact active policy epoch. Caller-provided
// record fields are not authority.
type PublicationAuthority interface {
	AuthorizeRegistration(context.Context, Record, VerifiedPublication) error
}

// Service registers, resolves, and transitions durable checkpoint records.
type Service struct {
	Repository Repository
	Policy     PublicationPolicy
	Clock      clock.Clock
	Verifier   CheckpointVerifier
	Authority  PublicationAuthority
}

func (s Service) now() time.Time {
	if s.Clock == nil {
		return time.Time{}
	}
	return s.Clock.Now().UTC()
}

// Register validates and seals an already committed checkpoint projection.
// The repository must reject an existing checkpoint ID; retries use the same
// immutable record or a new checkpoint ID rather than overwriting bytes.
func (s Service) Register(ctx context.Context, record Record) (Record, error) {
	if s.Repository == nil {
		return Record{}, invalid("checkpoint_repository_unavailable", "checkpoint repository is unavailable", nil)
	}
	if s.Clock == nil {
		return Record{}, invalid("checkpoint_clock_unavailable", "checkpoint registry clock is unavailable", nil)
	}
	if s.Verifier == nil {
		return Record{}, invalid("checkpoint_verifier_unavailable", "checkpoint commit verifier is unavailable", nil)
	}
	if s.Authority == nil {
		return Record{}, invalid("checkpoint_publication_authority_unavailable", "checkpoint publication authority is unavailable", nil)
	}
	if err := s.Policy.evaluateRegistration(record, s.now()); err != nil {
		return Record{}, err
	}
	verified, err := s.Verifier.VerifyCommittedCheckpoint(ctx, record)
	if err != nil {
		return Record{}, err
	}
	if err = verified.validateAgainst(record); err != nil {
		return Record{}, err
	}
	if err = s.Authority.AuthorizeRegistration(ctx, record, verified); err != nil {
		return Record{}, err
	}
	if err := record.SealDigest(); err != nil {
		return Record{}, err
	}
	if err := s.Repository.Create(ctx, record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (v VerifiedPublication) validateAgainst(record Record) error {
	if v.StageAttempt == 0 ||
		v.CheckpointID != record.CheckpointID ||
		v.RunID != record.RunID ||
		v.CheckpointAttempt != record.CheckpointAttempt ||
		v.FencingToken != record.FencingToken ||
		v.OptimizerStep != record.OptimizerStep ||
		!v.Manifest.EqualIdentity(record.Manifest) ||
		!v.Commit.EqualIdentity(record.Commit) ||
		!v.TopologyFingerprint.Equal(record.TopologyFingerprint) ||
		!equalOptionalArtifact(v.ParentManifest, record.ParentManifest) {
		return invalid("checkpoint_verified_facts_mismatch", "verified checkpoint bytes do not match the registry projection", nil)
	}
	return nil
}

func equalOptionalArtifact(left, right artifacts.Ref) bool {
	leftAbsent := left == (artifacts.Ref{})
	rightAbsent := right == (artifacts.Ref{})
	if leftAbsent || rightAbsent {
		return leftAbsent && rightAbsent
	}
	return left.EqualIdentity(right)
}

// Resolve returns a digest-verified record only while it is eligible for
// restore. A committed record whose retention deadline elapsed fails closed
// even if an asynchronous expiry projection has not run yet.
func (s Service) Resolve(ctx context.Context, checkpointID string) (Record, error) {
	if s.Repository == nil {
		return Record{}, invalid("checkpoint_repository_unavailable", "checkpoint repository is unavailable", nil)
	}
	if s.Clock == nil {
		return Record{}, invalid("checkpoint_clock_unavailable", "checkpoint registry clock is unavailable", nil)
	}
	if err := validateResourceID(checkpointID, "checkpoint"); err != nil {
		return Record{}, err
	}
	record, err := s.Repository.Get(ctx, checkpointID)
	if err != nil {
		return Record{}, err
	}
	if err = record.VerifyDigest(); err != nil {
		return Record{}, err
	}
	if record.CheckpointID != checkpointID {
		return Record{}, invalid("checkpoint_record_identity_mismatch", "checkpoint repository returned a different record", nil)
	}
	if !record.RestorableAt(s.now()) {
		return Record{}, invalid("checkpoint_not_restorable", "checkpoint lifecycle or retention does not permit restore", nil)
	}
	return record, nil
}

// Transition changes registry lifecycle with optimistic concurrency and
// reseals the record. expectedVersion is the version observed by the caller.
func (s Service) Transition(ctx context.Context, checkpointID string, next Lifecycle, expectedVersion uint64) (Record, error) {
	if s.Repository == nil {
		return Record{}, invalid("checkpoint_repository_unavailable", "checkpoint repository is unavailable", nil)
	}
	if s.Clock == nil {
		return Record{}, invalid("checkpoint_clock_unavailable", "checkpoint registry clock is unavailable", nil)
	}
	if err := validateResourceID(checkpointID, "checkpoint"); err != nil {
		return Record{}, err
	}
	record, err := s.Repository.Get(ctx, checkpointID)
	if err != nil {
		return Record{}, err
	}
	if err = record.VerifyDigest(); err != nil {
		return Record{}, err
	}
	if record.CheckpointID != checkpointID || expectedVersion == 0 || record.ResourceVersion != expectedVersion {
		return Record{}, invalid("checkpoint_resource_version_mismatch", "checkpoint resource version does not match", nil)
	}
	if err = s.Policy.evaluateTransition(record, next, s.now()); err != nil {
		return Record{}, err
	}
	record.Lifecycle = next
	record.ResourceVersion++
	if err = record.SealDigest(); err != nil {
		return Record{}, err
	}
	if err = s.Repository.CompareAndSwap(ctx, record, expectedVersion); err != nil {
		return Record{}, err
	}
	return record, nil
}
