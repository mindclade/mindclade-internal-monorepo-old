// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package leadership

import "errors"

var (
	ErrInvalidConfig  = errors.New("leadership: invalid configuration")
	ErrNotLeader      = errors.New("leadership: not leader")
	ErrLeadershipLost = errors.New("leadership: leadership lost")
	ErrAlreadyRun     = errors.New("leadership: elector already run")
)
