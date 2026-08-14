// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package grpcx

import "errors"

var (
	ErrInvalidConfig             = errors.New("grpcx: invalid configuration")
	ErrInvalidMethod             = errors.New("grpcx: invalid method")
	ErrNilListener               = errors.New("grpcx: nil listener")
	ErrNotServing                = errors.New("grpcx: server is not serving")
	ErrTransportSecurityRequired = errors.New("grpcx: transport security is required")
)
