// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package ingestion

import (
	"mindclade.internal/control/artifacts"
	"mindclade.internal/libs/go/identifiers"
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
