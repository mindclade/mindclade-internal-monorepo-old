// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package grpcx

import "errors"

var (
	ErrInvalidConfig             = errors.New("grpcx: invalid configuration")
	ErrInvalidMethod             = errors.New("grpcx: invalid method")
	ErrNilListener               = errors.New("grpcx: nil listener")
	ErrNotServing                = errors.New("grpcx: server is not serving")
	ErrTransportSecurityRequired = errors.New("grpcx: transport security is required")
)
