// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package lease

import "errors"

var (
	ErrInvalidKey   = errors.New("lease: invalid key")
	ErrInvalidOwner = errors.New("lease: invalid owner")
	ErrInvalidLease = errors.New("lease: invalid lease")
	ErrHeld         = errors.New("lease: held")
	ErrNotFound     = errors.New("lease: not found")
	ErrStale        = errors.New("lease: stale token or version")
)
