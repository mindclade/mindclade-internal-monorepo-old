// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

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
