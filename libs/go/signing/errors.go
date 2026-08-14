// Copyright 2026 Mindclade. All rights reserved.
package signing

import "errors"

var (
	ErrInvalidAlgorithm = errors.New("signing: invalid algorithm")
	ErrInvalidKeyID     = errors.New("signing: invalid key id")
	ErrInvalidKey       = errors.New("signing: invalid key")
	ErrInvalidSignature = errors.New("signing: invalid signature")
	ErrUnknownKey       = errors.New("signing: unknown key")
	ErrInactiveKey      = errors.New("signing: inactive key")
	ErrVerification     = errors.New("signing: verification failed")
)
