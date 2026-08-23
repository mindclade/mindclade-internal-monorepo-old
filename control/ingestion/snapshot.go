// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package ingestion

import (
	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/identifiers"
	"time"
)

type Snapshot struct {
	SnapshotID   string
	Source       SourceSpec
	RawArtifact  artifacts.Ref
	SourceCursor string
	ObservedAt   time.Time
}

func (s Snapshot) Validate() error {
	id, err := identifiers.ParseID(s.SnapshotID)
	if err != nil || id.Kind().String() != "snapshot" {
		return invalid("snapshot_id_invalid", "snapshot id must be canonical snapshot identifier", err)
	}
	if err := s.Source.Validate(); err != nil {
		return err
	}
	if err := s.RawArtifact.Validate(); err != nil {
		return err
	}
	if s.SourceCursor == "" || len(s.SourceCursor) > 1024 || s.ObservedAt.IsZero() {
		return invalid("snapshot_invalid", "snapshot cursor and observation time are required", nil)
	}
	return nil
}

// SameObservation reports whether two snapshots record the same immutable fact.
// Everything a SnapshotID durably promises is compared: which source at which
// version, which cursor position, and which raw bytes. ObservedAt is excluded
// because a retried write of one observation may carry a later wall clock and
// is still the same fact.
func (s Snapshot) SameObservation(other Snapshot) bool {
	return s.SnapshotID == other.SnapshotID &&
		s.Source == other.Source &&
		s.RawArtifact.EqualIdentity(other.RawArtifact) &&
		s.SourceCursor == other.SourceCursor
}
