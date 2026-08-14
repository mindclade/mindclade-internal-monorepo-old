// Copyright 2026 Mindclade. All rights reserved.
package projector

import "errors"

var (
	ErrInvalidRequest = errors.New("invalid projector request")
	ErrStopped        = errors.New("projector stopped")
	ErrNoFence        = errors.New("projector has no active fencing epoch")
)
