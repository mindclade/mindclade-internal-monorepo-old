// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package cursor

import "errors"

var (
	ErrInvalidRequest = errors.New("invalid cursor request")
	ErrNotFound       = errors.New("cursor not found")
	ErrConflict       = errors.New("cursor compare-and-swap conflict")
	ErrRegression     = errors.New("cursor position regression")
	ErrStaleFence     = errors.New("cursor fencing token is stale")
)
