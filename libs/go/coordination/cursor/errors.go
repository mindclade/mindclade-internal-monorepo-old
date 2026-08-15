// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package cursor

import "errors"

var (
	ErrInvalidRequest = errors.New("invalid cursor request")
	ErrNotFound       = errors.New("cursor not found")
	ErrConflict       = errors.New("cursor compare-and-swap conflict")
	ErrRegression     = errors.New("cursor position regression")
	ErrStaleFence     = errors.New("cursor fencing token is stale")
)
