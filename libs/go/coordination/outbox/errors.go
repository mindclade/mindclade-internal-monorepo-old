// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package outbox

import "errors"

var (
	ErrInvalidMessage    = errors.New("outbox: invalid message")
	ErrInvalidClaim      = errors.New("outbox: invalid claim")
	ErrInvalidRequest    = errors.New("outbox: invalid request")
	ErrAlreadyExists     = errors.New("outbox: message already exists")
	ErrNotFound          = errors.New("outbox: message not found")
	ErrClaimLost         = errors.New("outbox: claim lost")
	ErrUnavailable       = errors.New("outbox: store unavailable")
	ErrPublishFailed     = errors.New("outbox: publish failed")
	ErrDispatcherStopped = errors.New("outbox: dispatcher stopped")
)

const (
	ReasonInvalidMessage = "invalid_outbox_message"
	ReasonInvalidClaim   = "invalid_outbox_claim"
	ReasonClaimLost      = "outbox_claim_lost"
	ReasonStoreFailed    = "outbox_store_failed"
	ReasonPublishFailed  = "outbox_publish_failed"
)
