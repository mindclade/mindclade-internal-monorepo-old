// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package projector

import "errors"

var (
	ErrInvalidRequest = errors.New("invalid projector request")
	ErrStopped        = errors.New("projector stopped")
	ErrNoFence        = errors.New("projector has no active fencing epoch")
)
