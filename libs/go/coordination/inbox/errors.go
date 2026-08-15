// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package inbox

import "errors"

var (
	ErrInvalidRequest = errors.New("invalid inbox request")
	ErrConflict       = errors.New("inbox identity conflicts with a different payload")
	ErrInProgress     = errors.New("inbox message is already being processed")
	ErrTransaction    = errors.New("inbox transaction failed")
)
