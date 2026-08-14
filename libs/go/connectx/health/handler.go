// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package health

import (
	"net/http"

	"connectrpc.com/connect"
	"connectrpc.com/grpchealth"
)

// NewHandler constructs the standard gRPC health handler and mount pattern.
func NewHandler(checker grpchealth.Checker, options ...connect.HandlerOption) (string, http.Handler) {
	return grpchealth.NewHandler(checker, options...)
}
