// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package pagination

import "errors"

var (
	ErrInvalidRequest = errors.New("pagination: invalid request")
	ErrInvalidCursor  = errors.New("pagination: invalid cursor")
	ErrExpiredCursor  = errors.New("pagination: expired cursor")
	ErrCursorMismatch = errors.New("pagination: cursor binding mismatch")
)
