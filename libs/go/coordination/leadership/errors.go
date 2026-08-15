// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package leadership

import "errors"

var (
	ErrInvalidConfig  = errors.New("leadership: invalid configuration")
	ErrNotLeader      = errors.New("leadership: not leader")
	ErrLeadershipLost = errors.New("leadership: leadership lost")
	ErrAlreadyRun     = errors.New("leadership: elector already run")
)
