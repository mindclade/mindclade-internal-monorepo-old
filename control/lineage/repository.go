// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package lineage

import (
	"context"

	"go.mindclade.dev/libs/go/identifiers"
)

// Repository persists immutable graphs. Implementations must make Put idempotent
// for the same digest and reject any attempt to bind different bytes to it.
type Repository interface {
	Put(context.Context, identifiers.Digest, Graph) error
	Get(context.Context, identifiers.Digest) (Graph, error)
}
