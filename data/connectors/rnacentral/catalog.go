// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package rnacentral

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
		return errors.New("rnacentral catalog entry requires objects")
	}
	snapshot, err := BuildSnapshot(e, connectors.Cursor{Value: e.Release, Sequence: 1}, e.Objects[0].UpdatedAt)
	if err != nil {
		return err
	}
	return snapshot.Validate()
}
