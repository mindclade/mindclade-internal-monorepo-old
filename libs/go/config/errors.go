// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package config

import "errors"

var (
	ErrInvalidField     = errors.New("config: invalid field")
	ErrUnknownKey       = errors.New("config: unknown key")
	ErrRequiredMissing  = errors.New("config: required value missing")
	ErrInvalidValue     = errors.New("config: invalid value")
	ErrSourceFailure    = errors.New("config: source failure")
	ErrRestartRequired  = errors.New("config: restart required")
	ErrSnapshotMismatch = errors.New("config: snapshot mismatch")
)
