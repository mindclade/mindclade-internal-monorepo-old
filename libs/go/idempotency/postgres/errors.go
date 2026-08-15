// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import "errors"

var (
	ErrInvalidConfig = errors.New("idempotency postgres: invalid configuration")
	ErrLeaseLost     = errors.New("idempotency postgres: lease lost")
	ErrRecordMissing = errors.New("idempotency postgres: record missing")
)
