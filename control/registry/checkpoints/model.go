// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package checkpoints is the durable catalog authority for committed training
// checkpoints. It records immutable manifest and commit artifacts plus bounded
// retention state; Python owns checkpoint state semantics and Rust owns byte
// transfer and verification mechanics.
package checkpoints

import (
	"time"

	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/identifiers"
)

// Lifecycle is the durable registry state of a committed checkpoint.
type Lifecycle string

const (
	LifecycleCommitted Lifecycle = "committed"
	LifecycleProtected Lifecycle = "protected"
	LifecycleExpired   Lifecycle = "expired"
	LifecycleRevoked   Lifecycle = "revoked"
)

// Valid reports whether the lifecycle is a declared registry state.
func (l Lifecycle) Valid() bool {
	switch l {
	case LifecycleCommitted, LifecycleProtected, LifecycleExpired, LifecycleRevoked:
		return true
	}
	return false
}

// Record is the Go policy projection of mindclade.registry.v1.CheckpointRecord.
// Provider locations are deliberately absent: ArtifactRef identity is immutable
// and placement is resolved through the artifact catalog.
type Record struct {
	RecordDigest        identifiers.Digest `json:"record_digest,omitempty"`
	CheckpointID        string             `json:"checkpoint_id"`
	RunID               string             `json:"run_id"`
	Manifest            artifacts.Ref      `json:"manifest"`
	Commit              artifacts.Ref      `json:"commit"`
	OptimizerStep       uint64             `json:"optimizer_step"`
	CheckpointAttempt   uint64             `json:"checkpoint_attempt"`
	FencingToken        uint64             `json:"fencing_token"`
	TopologyFingerprint identifiers.Digest `json:"topology_fingerprint"`
	ParentManifest      artifacts.Ref      `json:"parent_manifest,omitempty"`
	Lifecycle           Lifecycle          `json:"lifecycle"`
	Created             time.Time          `json:"created"`
	RetainUntil         time.Time          `json:"retain_until"`
	PolicyEpoch         uint64             `json:"policy_epoch"`
	ResourceVersion     uint64             `json:"resource_version"`
}
