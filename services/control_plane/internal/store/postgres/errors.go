// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import "errors"

var (
	// ErrInvalidConfig is returned when a store is constructed with an unusable
	// database handle, clock, or table name.
	ErrInvalidConfig = errors.New("registry postgres: invalid configuration")

	// ErrDescriptorMissing is returned when a descriptor digest resolves to no
	// row. It is joined into a faults.CodeNotFound error so a caller cannot
	// mistake absence for a zero-valued descriptor.
	ErrDescriptorMissing = errors.New("registry postgres: model descriptor missing")

	// ErrDigestCollision is returned when a descriptor digest is already stored
	// against different canonical content. This is a seal failure, not a
	// concurrent write, and is never retryable.
	ErrDigestCollision = errors.New("registry postgres: descriptor digest collision")

	// ErrReleaseConflict is returned when a release write loses its
	// compare-and-swap on ResourceVersion.
	ErrReleaseConflict = errors.New("registry postgres: release resource version conflict")

	// ErrGraphImmutable is returned when an evidence graph is rewritten under a
	// release identifier that already holds a different graph.
	ErrGraphImmutable = errors.New("registry postgres: evidence graph is immutable")

	// ErrEvidenceImmutable is returned when a sealed ledger identity already
	// exists with different signed content.
	ErrEvidenceImmutable = errors.New("registry postgres: evidence ledger record is immutable")
)
