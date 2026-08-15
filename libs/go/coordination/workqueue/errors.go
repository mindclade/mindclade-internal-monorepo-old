// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package workqueue

import "errors"

var (
	ErrInvalidRequest = errors.New("invalid work queue request")
	ErrAlreadyExists  = errors.New("work item already exists")
	ErrNotFound       = errors.New("work item not found")
	ErrLeaseLost      = errors.New("work item lease lost")
	ErrTerminal       = errors.New("work item is terminal")
	ErrWorkerStopped  = errors.New("work queue worker stopped")
)
