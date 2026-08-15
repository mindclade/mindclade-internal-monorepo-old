// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

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
