// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package messaging

import (
	"errors"

	"mindclade.internal/libs/go/faults"
)

var (
	ErrInvalidMessage      = errors.New("messaging: invalid message")
	ErrInvalidSubscription = errors.New("messaging: invalid subscription")
	ErrClosed              = errors.New("messaging: closed")
	ErrCapacityExceeded    = errors.New("messaging: capacity exceeded")
	ErrAlreadySettled      = errors.New("messaging: delivery already settled")
	ErrPublishFailed       = errors.New("messaging: publish failed")
	ErrReceiveFailed       = errors.New("messaging: receive failed")
)

func invalid(cause error, reason, operation string, fields faults.Fields) error {
	if cause == nil {
		cause = ErrInvalidMessage
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, "invalid messaging value",
		faults.WithReason(reason), faults.WithOperation(operation), faults.WithFields(fields), faults.WithRetryPolicy(faults.NoRetry()))
}

func unavailable(cause error, reason, operation string, fields faults.Fields) error {
	if cause == nil {
		cause = ErrReceiveFailed
	}
	return faults.Wrap(cause, faults.CodeUnavailable, "messaging operation unavailable",
		faults.WithReason(reason), faults.WithOperation(operation), faults.WithFields(fields), faults.WithRetryPolicy(faults.BackoffRetry(0)))
}
