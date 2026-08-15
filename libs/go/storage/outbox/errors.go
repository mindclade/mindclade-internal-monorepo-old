// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outbox

import coordination "mindclade.internal/libs/go/coordination/outbox"

var (
	ErrInvalidMessage    = coordination.ErrInvalidMessage
	ErrInvalidClaim      = coordination.ErrInvalidClaim
	ErrInvalidRequest    = coordination.ErrInvalidRequest
	ErrAlreadyExists     = coordination.ErrAlreadyExists
	ErrNotFound          = coordination.ErrNotFound
	ErrClaimLost         = coordination.ErrClaimLost
	ErrUnavailable       = coordination.ErrUnavailable
	ErrPublishFailed     = coordination.ErrPublishFailed
	ErrDispatcherStopped = coordination.ErrDispatcherStopped
)

const (
	ReasonInvalidMessage = coordination.ReasonInvalidMessage
	ReasonInvalidClaim   = coordination.ReasonInvalidClaim
	ReasonClaimLost      = coordination.ReasonClaimLost
	ReasonStoreFailed    = coordination.ReasonStoreFailed
	ReasonPublishFailed  = coordination.ReasonPublishFailed
)
