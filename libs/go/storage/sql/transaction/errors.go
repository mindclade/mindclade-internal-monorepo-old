// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package transaction

import "errors"

var (
	ErrInvalidRequest = errors.New("transaction: invalid request")
	ErrNested         = errors.New("transaction: nested transaction")
	ErrBegin          = errors.New("transaction: begin failed")
	ErrCommit         = errors.New("transaction: commit failed")
	ErrRollback       = errors.New("transaction: rollback failed")
)
