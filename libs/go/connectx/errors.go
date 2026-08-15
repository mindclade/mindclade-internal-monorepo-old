// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package connectx

import "errors"

var (
	ErrInvalidConfig       = errors.New("connectx: invalid configuration")
	ErrInvalidProtocol     = errors.New("connectx: invalid protocol")
	ErrInvalidProcedure    = errors.New("connectx: invalid procedure")
	ErrInvalidWireMetadata = errors.New("connectx: invalid wire metadata")
	ErrNilMux              = errors.New("connectx: nil mux")
	ErrNilHandler          = errors.New("connectx: nil handler")
)
