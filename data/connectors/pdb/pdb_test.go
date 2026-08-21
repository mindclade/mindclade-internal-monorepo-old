// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package pdb

import (
	"testing"
	"time"

	"go.mindclade.dev/data/connectors"
)

func TestBuildSnapshotSortsObjectsAndRequiresApprovedUse(t *testing.T) {
	entry := CatalogEntry{
		Release: "2026-08",
		Objects: []connectors.Object{
			{URI: "https://example.invalid/pdb/z", Generation: "2", SizeBytes: 2, UpdatedAt: time.Unix(2, 0).UTC()},
			{URI: "https://example.invalid/pdb/a", Generation: "1", SizeBytes: 1, UpdatedAt: time.Unix(1, 0).UTC()},
		},
		License: LicensePolicy{Reference: "approved-ref", ApprovedUses: []string{"research"}},
	}
	snapshot, err := BuildSnapshot(entry, connectors.Cursor{Value: "2026-08", Sequence: 1}, time.Unix(3, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Objects[0].URI != "https://example.invalid/pdb/a" {
		t.Fatalf("objects not sorted: %#v", snapshot.Objects)
	}
	if err := entry.License.ValidateUse("clinical"); err == nil {
		t.Fatal("expected unapproved use rejection")
	}
}
