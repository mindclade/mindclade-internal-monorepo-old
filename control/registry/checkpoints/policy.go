// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package checkpoints reserves the boundary defined by the production blueprint.
package checkpoints

import "time"

// PublicationPolicy bounds new checkpoint records and lifecycle transitions.
// CheckpointVerifier and PublicationAuthority independently establish typed
// byte integrity and current admission authority before registration.
type PublicationPolicy struct {
	ActivePolicyEpoch uint64
	MinimumRetention  time.Duration
	MaximumRetention  time.Duration
	MaximumManifest   uint64
	MaximumCommit     uint64
}

// ProductionPolicy returns the fail-closed registry defaults. Environment
// retention policy may be stricter, but must not exceed these resource bounds.
func ProductionPolicy() PublicationPolicy {
	return PublicationPolicy{
		MinimumRetention: time.Hour,
		MaximumRetention: 365 * 24 * time.Hour,
		MaximumManifest:  4 << 20,
		MaximumCommit:    1 << 20,
	}
}

func (p PublicationPolicy) evaluateRegistration(record Record, now time.Time) error {
	if err := record.Validate(); err != nil {
		return err
	}
	if p.ActivePolicyEpoch == 0 {
		return invalid("checkpoint_policy_unconfigured", "checkpoint publication policy authority is not configured", nil)
	}
	if record.Lifecycle != LifecycleCommitted || record.ResourceVersion != 1 {
		return invalid("checkpoint_initial_state_invalid", "new checkpoint records must begin committed at resource version one", nil)
	}
	if record.PolicyEpoch != p.ActivePolicyEpoch {
		return invalid("checkpoint_policy_epoch_mismatch", "checkpoint record was not produced under the active policy epoch", nil)
	}
	retention := record.RetainUntil.Sub(record.Created)
	if p.MinimumRetention <= 0 || p.MaximumRetention < p.MinimumRetention || retention < p.MinimumRetention || retention > p.MaximumRetention {
		return invalid("checkpoint_retention_out_of_policy", "checkpoint retention is outside policy bounds", nil)
	}
	if record.Created.After(now) {
		return invalid("checkpoint_created_in_future", "checkpoint creation time is in the future", nil)
	}
	if !record.RetainUntil.After(now) {
		return invalid("checkpoint_retention_elapsed", "a newly registered checkpoint must remain inside retention", nil)
	}
	if record.Manifest.SizeBytes == 0 || record.Manifest.SizeBytes > p.MaximumManifest {
		return invalid("checkpoint_manifest_size_out_of_policy", "checkpoint manifest size is outside policy bounds", nil)
	}
	if record.Commit.SizeBytes == 0 || record.Commit.SizeBytes > p.MaximumCommit {
		return invalid("checkpoint_commit_size_out_of_policy", "checkpoint commit size is outside policy bounds", nil)
	}
	return nil
}

func (p PublicationPolicy) evaluateTransition(record Record, next Lifecycle, now time.Time) error {
	if !next.Valid() || next == record.Lifecycle {
		return invalid("checkpoint_lifecycle_transition_invalid", "checkpoint lifecycle transition is not declared", nil)
	}
	switch record.Lifecycle {
	case LifecycleCommitted:
		if next == LifecycleProtected || next == LifecycleRevoked {
			return nil
		}
		if next == LifecycleExpired && !now.Before(record.RetainUntil) {
			return nil
		}
	case LifecycleProtected:
		if next == LifecycleCommitted || next == LifecycleRevoked {
			return nil
		}
	}
	return invalid("checkpoint_lifecycle_transition_invalid", "checkpoint lifecycle transition is not permitted", nil)
}
