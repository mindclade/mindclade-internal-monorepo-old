// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package resourceversion

import "errors"

var (
	ErrInvalidVersion      = errors.New("resourceversion: invalid version")
	ErrInvalidPrecondition = errors.New("resourceversion: invalid precondition")
	ErrPreconditionFailed  = errors.New("resourceversion: precondition failed")
)
