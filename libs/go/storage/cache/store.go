// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package cache

import "context"

type Store interface {
	Get(context.Context, Key) (Entry, error)
	Set(context.Context, Key, []byte, SetOptions) (Entry, error)
	Delete(context.Context, Key, DeleteOptions) error
}
