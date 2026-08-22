// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package checkpoints reserves the boundary defined by the production blueprint.
package checkpoints

import (
	"strconv"
	"strings"
	"time"

	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

const (
	checkpointRecordDocument = "checkpoint-registry-record/v1"
	checkpointSchemaVersion  = 1
	maximumTextBytes         = 4096
	manifestLogicalKind      = "training.checkpoint.manifest"
	commitLogicalKind        = "artifact.checkpoint.commit"
)

func invalid(reason, message string, cause error) error {
	options := []faults.Option{
		faults.WithReason(reason),
		faults.WithOperation("control.registry.checkpoints"),
		faults.WithRetryPolicy(faults.NoRetry()),
	}
	if cause == nil {
		return faults.New(faults.CodeInvalidArgument, message, options...)
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, message, options...)
}

func canonicalText(value string) bool {
	return value != "" && len(value) <= maximumTextBytes && !strings.ContainsAny(value, "\n|")
}

func validateResourceID(value, expectedKind string) error {
	id, err := identifiers.ParseID(value)
	if err != nil || id.Kind().String() != expectedKind {
		return invalid("checkpoint_resource_id_invalid", "checkpoint and run identifiers must be canonical and have the expected kind", err)
	}
	return nil
}

func validateArtifact(ref artifacts.Ref, logicalKind string) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	if !canonicalText(ref.MediaType) {
		return invalid("checkpoint_artifact_media_type_invalid", "checkpoint artifact media type is empty, oversized, or contains a reserved delimiter", nil)
	}
	if ref.LogicalKind != logicalKind || ref.SchemaVersion != checkpointSchemaVersion {
		return invalid("checkpoint_artifact_contract_invalid", "checkpoint artifact kind or schema version is invalid", nil)
	}
	return nil
}

// Validate checks a checkpoint record independently of its sealed digest.
func (r Record) Validate() error {
	if err := validateResourceID(r.CheckpointID, "checkpoint"); err != nil {
		return err
	}
	if err := validateResourceID(r.RunID, "run"); err != nil {
		return err
	}
	if err := validateArtifact(r.Manifest, manifestLogicalKind); err != nil {
		return err
	}
	if err := validateArtifact(r.Commit, commitLogicalKind); err != nil {
		return err
	}
	if r.Manifest.Digest.Equal(r.Commit.Digest) {
		return invalid("checkpoint_artifact_identity_collision", "manifest and commit artifacts must have distinct identities", nil)
	}
	if r.CheckpointAttempt == 0 || r.FencingToken == 0 {
		return invalid("checkpoint_fencing_invalid", "checkpoint attempt and fencing token must be positive", nil)
	}
	if !r.TopologyFingerprint.Valid() {
		return invalid("checkpoint_topology_fingerprint_invalid", "checkpoint topology fingerprint is required", nil)
	}
	if r.ParentManifest.Digest.Valid() {
		if err := validateArtifact(r.ParentManifest, manifestLogicalKind); err != nil {
			return err
		}
		if r.ParentManifest.EqualIdentity(r.Manifest) {
			return invalid("checkpoint_parent_cycle", "checkpoint manifest cannot be its own parent", nil)
		}
	} else if r.ParentManifest != (artifacts.Ref{}) {
		return invalid("checkpoint_parent_incomplete", "checkpoint parent manifest must be absent or complete", nil)
	}
	if !r.Lifecycle.Valid() {
		return invalid("checkpoint_lifecycle_invalid", "checkpoint lifecycle is not declared", nil)
	}
	if r.Created.IsZero() || !r.RetainUntil.After(r.Created) {
		return invalid("checkpoint_retention_invalid", "checkpoint retention must end after creation", nil)
	}
	if r.Created.Nanosecond()%int(time.Millisecond) != 0 || r.RetainUntil.Nanosecond()%int(time.Millisecond) != 0 {
		return invalid("checkpoint_timestamp_precision_invalid", "checkpoint timestamps must use canonical millisecond precision", nil)
	}
	if r.PolicyEpoch == 0 || r.ResourceVersion == 0 {
		return invalid("checkpoint_version_invalid", "checkpoint policy epoch and resource version must be positive", nil)
	}
	return nil
}

// CanonicalBytes returns an injective newline-framed record encoding. Artifact
// placement is excluded and the record digest itself is omitted.
func (r Record) CanonicalBytes() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	var out strings.Builder
	out.WriteString(checkpointRecordDocument)
	out.WriteByte('\n')
	for _, value := range []string{
		r.CheckpointID,
		r.RunID,
		strconv.FormatUint(r.OptimizerStep, 10),
		strconv.FormatUint(r.CheckpointAttempt, 10),
		strconv.FormatUint(r.FencingToken, 10),
		r.TopologyFingerprint.String(),
		string(r.Lifecycle),
		strconv.FormatInt(r.Created.UTC().UnixMilli(), 10),
		strconv.FormatInt(r.RetainUntil.UTC().UnixMilli(), 10),
		strconv.FormatUint(r.PolicyEpoch, 10),
		strconv.FormatUint(r.ResourceVersion, 10),
	} {
		if !canonicalText(value) {
			return nil, invalid("checkpoint_canonical_field_invalid", "checkpoint canonical field is empty, oversized, or contains a reserved delimiter", nil)
		}
		out.WriteString(value)
		out.WriteByte('\n')
	}
	writeArtifact(&out, "manifest", r.Manifest)
	writeArtifact(&out, "commit", r.Commit)
	if r.ParentManifest.Digest.Valid() {
		writeArtifact(&out, "parent", r.ParentManifest)
	} else {
		out.WriteString("parent|-\n")
	}
	return []byte(out.String()), nil
}

func writeArtifact(out *strings.Builder, label string, ref artifacts.Ref) {
	out.WriteString(strings.Join([]string{
		label,
		ref.Digest.String(),
		strconv.FormatUint(ref.SizeBytes, 10),
		ref.MediaType,
		ref.LogicalKind,
		strconv.FormatUint(uint64(ref.SchemaVersion), 10),
	}, "|"))
	out.WriteByte('\n')
}

// SealDigest computes and stores the immutable record digest.
func (r *Record) SealDigest() error {
	if r == nil {
		return invalid("checkpoint_record_required", "checkpoint record is required", nil)
	}
	payload, err := r.CanonicalBytes()
	if err != nil {
		return err
	}
	r.RecordDigest = identifiers.SHA256(payload)
	return nil
}

// VerifyDigest rejects unsealed or mutated repository state.
func (r Record) VerifyDigest() error {
	payload, err := r.CanonicalBytes()
	if err != nil {
		return err
	}
	if !r.RecordDigest.Valid() || !r.RecordDigest.Equal(identifiers.SHA256(payload)) {
		return invalid("checkpoint_record_digest_mismatch", "checkpoint record digest is absent or does not match its content", nil)
	}
	return nil
}

// RestorableAt reports whether lifecycle and retention allow restore at now.
func (r Record) RestorableAt(now time.Time) bool {
	if r.Lifecycle != LifecycleCommitted && r.Lifecycle != LifecycleProtected {
		return false
	}
	return r.Lifecycle == LifecycleProtected || now.Before(r.RetainUntil)
}
