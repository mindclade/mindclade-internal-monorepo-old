// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package coordination

import "errors"

var (
	ErrInvalidClaim   = errors.New("coordination: invalid claim")
	ErrInvalidFailure = errors.New("coordination: invalid failure")
	ErrStaleClaim     = errors.New("coordination: stale claim")
	ErrClaimLost      = errors.New("coordination: claim lost")
)
