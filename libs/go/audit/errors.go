// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package audit

import "errors"

var (
	ErrInvalidAction   = errors.New("audit: invalid action")
	ErrInvalidActor    = errors.New("audit: invalid actor")
	ErrInvalidTarget   = errors.New("audit: invalid target")
	ErrInvalidChange   = errors.New("audit: invalid change")
	ErrInvalidFields   = errors.New("audit: invalid fields")
	ErrInvalidEvent    = errors.New("audit: invalid event")
	ErrNilFactory      = errors.New("audit: nil factory dependency")
	ErrNilRecorder     = errors.New("audit: nil recorder")
	ErrNilContext      = errors.New("audit: nil context")
	ErrRecorderFailure = errors.New("audit: recorder failure")
)
