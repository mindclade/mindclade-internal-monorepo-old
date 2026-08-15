// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package retry

import "time"

// Attempt describes one invocation of an operation.
type Attempt struct {
	number    int
	startedAt time.Time
}

// Number returns the one-based attempt number.
func (attempt Attempt) Number() int { return attempt.number }

// RetryNumber returns zero for the first attempt, one for the first retry, and
// so on.
func (attempt Attempt) RetryNumber() int {
	if attempt.number <= 1 {
		return 0
	}
	return attempt.number - 1
}

// First reports whether this is the initial attempt.
func (attempt Attempt) First() bool { return attempt.number == 1 }

// StartedAt returns the attempt start time.
func (attempt Attempt) StartedAt() time.Time { return attempt.startedAt }
