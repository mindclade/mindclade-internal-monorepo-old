// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package clock

import "errors"

var (
	// ErrNilContext is returned when a context-aware operation receives a nil
	// context. Callers should pass context.Background when no cancellation is
	// required.
	ErrNilContext = errors.New("clock: nil context")

	// ErrTimeReversal is returned when a FakeClock is asked to move backward.
	ErrTimeReversal = errors.New("clock: cannot move time backward")
)
