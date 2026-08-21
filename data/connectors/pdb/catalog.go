// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package pdb

import (
	"errors"

	"go.mindclade.dev/data/connectors"
)

type CatalogEntry struct {
	Release string
	Objects []connectors.Object
	License LicensePolicy
}

func (e CatalogEntry) Validate() error {
	if len(e.Objects) == 0 {
		return errors.New("pdb catalog entry requires objects")
	}
	snapshot := connectors.Snapshot{
		Source: "pdb", Version: e.Release, Cursor: connectors.Cursor{Value: e.Release, Sequence: 1},
		Objects: e.Objects, ObservedAt: e.Objects[0].UpdatedAt, LicenseRef: e.License.Reference,
	}
	return snapshot.Validate()
}
