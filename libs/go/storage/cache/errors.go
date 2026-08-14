// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package cache

import "errors"

var (
	ErrInvalidKey      = errors.New("cache: invalid key")
	ErrInvalidEntry    = errors.New("cache: invalid entry")
	ErrInvalidOptions  = errors.New("cache: invalid options")
	ErrMiss            = errors.New("cache: miss")
	ErrVersionMismatch = errors.New("cache: version mismatch")
	ErrEntryTooLarge   = errors.New("cache: entry too large")
)
