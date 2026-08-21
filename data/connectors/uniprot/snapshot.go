// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package uniprot

import (
	"cmp"
	"slices"
	"time"

	"go.mindclade.dev/data/connectors"
)

type Snapshot = connectors.Snapshot

func BuildSnapshot(entry CatalogEntry, cursor connectors.Cursor, observedAt time.Time) (connectors.Snapshot, error) {
	objects := slices.Clone(entry.Objects)
	slices.SortFunc(objects, func(left, right connectors.Object) int {
		return cmp.Compare(left.URI+"\x00"+left.Generation, right.URI+"\x00"+right.Generation)
	})
	snapshot := connectors.Snapshot{
		Source: "uniprot", Version: entry.Release, Cursor: cursor, Objects: objects,
		ObservedAt: observedAt, LicenseRef: entry.License.Reference,
	}
	return snapshot, snapshot.Validate()
}
