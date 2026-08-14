// Copyright 2026 Mindclade. All rights reserved.
package pagination

import "errors"

var (
	ErrInvalidRequest = errors.New("pagination: invalid request")
	ErrInvalidCursor  = errors.New("pagination: invalid cursor")
	ErrExpiredCursor  = errors.New("pagination: expired cursor")
	ErrCursorMismatch = errors.New("pagination: cursor binding mismatch")
)
