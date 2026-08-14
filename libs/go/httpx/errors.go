// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package httpx

import "errors"

var (
	ErrNilHandler           = errors.New("httpx: nil handler")
	ErrNilListener          = errors.New("httpx: nil listener")
	ErrInvalidConfig        = errors.New("httpx: invalid configuration")
	ErrRequestTooLarge      = errors.New("httpx: request exceeds configured limit")
	ErrResponseTooLarge     = errors.New("httpx: response exceeds configured limit")
	ErrInvalidResponse      = errors.New("httpx: invalid response")
	ErrUnsupportedMediaType = errors.New("httpx: unsupported media type")
	ErrNotServing           = errors.New("httpx: server is not serving")
)
