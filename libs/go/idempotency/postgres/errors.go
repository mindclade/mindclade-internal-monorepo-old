// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package postgres

import "errors"

var (
	ErrInvalidConfig = errors.New("idempotency postgres: invalid configuration")
	ErrLeaseLost     = errors.New("idempotency postgres: lease lost")
	ErrRecordMissing = errors.New("idempotency postgres: record missing")
)
