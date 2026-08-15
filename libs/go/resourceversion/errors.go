// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package resourceversion

import "errors"

var (
	ErrInvalidVersion      = errors.New("resourceversion: invalid version")
	ErrInvalidPrecondition = errors.New("resourceversion: invalid precondition")
	ErrPreconditionFailed  = errors.New("resourceversion: precondition failed")
)
