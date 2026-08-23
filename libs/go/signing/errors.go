// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package signing

import "errors"

var (
	ErrInvalidAlgorithm = errors.New("signing: invalid algorithm")
	ErrInvalidDomain    = errors.New("signing: invalid domain")
	ErrInvalidKeyID     = errors.New("signing: invalid key id")
	ErrInvalidKey       = errors.New("signing: invalid key")
	ErrInvalidSignature = errors.New("signing: invalid signature")
	ErrUnknownKey       = errors.New("signing: unknown key")
	ErrInactiveKey      = errors.New("signing: inactive key")
	ErrVerification     = errors.New("signing: verification failed")
)
