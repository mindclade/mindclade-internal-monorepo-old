// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package cursor

import "context"

type Store interface {
	Load(context.Context, Key) (Cursor, error)
	Advance(context.Context, AdvanceRequest) (Cursor, error)
	Delete(context.Context, Key, uint64) error
}
