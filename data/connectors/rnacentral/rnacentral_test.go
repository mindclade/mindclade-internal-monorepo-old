// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package rnacentral

import (
	"testing"
	"time"

	"go.mindclade.dev/data/connectors"
)

func TestBuildSnapshotRetainsReleaseAndLicense(t *testing.T) {
	entry := CatalogEntry{
		Release: "2026-08",
		Objects: []connectors.Object{{
			URI: "https://example.invalid/rnacentral/release.fasta", Generation: "etag-1",
			SizeBytes: 1, UpdatedAt: time.Unix(1, 0).UTC(),
		}},
		License: LicensePolicy{Reference: "approved-ref", ApprovedUses: []string{"research"}},
	}
	snapshot, err := BuildSnapshot(entry, connectors.Cursor{Value: "2026-08", Sequence: 1}, time.Unix(2, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Version != entry.Release || snapshot.LicenseRef != entry.License.Reference {
		t.Fatalf("release evidence lost: %#v", snapshot)
	}
}
