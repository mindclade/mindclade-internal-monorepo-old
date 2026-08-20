// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package idempotency

import (
	"errors"

	"go.mindclade.dev/libs/go/faults"
)

var (
	ErrInvalidKey         = errors.New("idempotency: invalid key")
	ErrInvalidScope       = errors.New("idempotency: invalid scope")
	ErrInvalidIdentity    = errors.New("idempotency: invalid identity")
	ErrInvalidFingerprint = errors.New("idempotency: invalid fingerprint")
	ErrInvalidResult      = errors.New("idempotency: invalid result")
	ErrInvalidRecord      = errors.New("idempotency: invalid record")
	ErrInvalidLease       = errors.New("idempotency: invalid lease")
	ErrInvalidRequest     = errors.New("idempotency: invalid request")
	ErrNilContext         = errors.New("idempotency: nil context")
	ErrNilStore           = errors.New("idempotency: nil store")
	ErrNilOperation       = errors.New("idempotency: nil operation")
	ErrKeyConflict        = errors.New("idempotency: key reused with different request")
	ErrInProgress         = errors.New("idempotency: request is already in progress")
	ErrLeaseLost          = errors.New("idempotency: lease is no longer owned")
	ErrNotFound           = errors.New("idempotency: record not found")
	ErrCommitFailed       = errors.New("idempotency: result commit failed")
)

const (
	ReasonInvalidKey             = "invalid_idempotency_key"
	ReasonInvalidScope           = "invalid_idempotency_scope"
	ReasonInvalidIdentity        = "invalid_idempotency_identity"
	ReasonInvalidFingerprint     = "invalid_idempotency_fingerprint"
	ReasonInvalidResult          = "invalid_idempotency_result"
	ReasonInvalidRecord          = "invalid_idempotency_record"
	ReasonInvalidLease           = "invalid_idempotency_lease"
	ReasonInvalidRequest         = "invalid_idempotency_request"
	ReasonKeyConflict            = "idempotency_key_conflict"
	ReasonInProgress             = "idempotency_in_progress"
	ReasonLeaseLost              = "idempotency_lease_lost"
	ReasonNotFound               = "idempotency_record_not_found"
	ReasonStoreFailed            = "idempotency_store_failed"
	ReasonCommitFailed           = "idempotency_commit_failed"
	ReasonReleaseFailed          = "idempotency_release_failed"
	ReasonInvalidOperationResult = "idempotency_invalid_operation_result"
)

func invalid(cause error, reason, message, operation string, fields faults.Fields) error {
	return faults.Wrap(
		cause,
		faults.CodeInvalidArgument,
		message,
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithFields(fields),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
