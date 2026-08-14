// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package coordination

import "errors"

var (
	ErrInvalidClaim   = errors.New("coordination: invalid claim")
	ErrInvalidFailure = errors.New("coordination: invalid failure")
	ErrStaleClaim     = errors.New("coordination: stale claim")
	ErrClaimLost      = errors.New("coordination: claim lost")
)
