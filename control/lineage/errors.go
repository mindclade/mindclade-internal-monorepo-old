// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package lineage

import "errors"

// Reason codes every Repository implementation must use for the two rejections
// the contract defines. A caller decides what to do from the reason, so an
// in-memory repository and a durable one that disagreed on the string would
// make the seam untestable: a test written against one would silently pass
// against the other while asserting nothing.
const (
	ReasonGraphNotFound  = "lineage_graph_not_found"
	ReasonGraphImmutable = "lineage_graph_immutable"
)

// Sentinels behind those reasons. They exist so an adapter in another package
// can produce the identical rejection -- same reason, same errors.Is target --
// without re-deriving the domain's wording.
var (
	// ErrGraphNotFound reports a digest that holds no stored graph. It is a
	// distinct sentinel from a corrupt read so that "never published" cannot be
	// mistaken for "published and since lost".
	ErrGraphNotFound = errors.New("lineage: graph was not found")

	// ErrGraphImmutable reports a digest already bound to a different graph.
	// The binding is what makes a quoted digest meaningful -- a release cites
	// provenance by digest -- so rebinding would silently repoint every
	// existing citation. It is never transient and never retryable.
	ErrGraphImmutable = errors.New("lineage: graph digest is already bound to a different graph")
)
