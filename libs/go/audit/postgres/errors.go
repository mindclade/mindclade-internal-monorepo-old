// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package postgres

import "errors"

var ErrConflict = errors.New("audit postgres: conflicting event identifier")
