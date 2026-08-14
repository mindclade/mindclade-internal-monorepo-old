// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package interceptors

import (
	"context"

	"mindclade.internal/libs/go/auth"
)

// Validator is implemented by request messages with transport-boundary
// validation. Generated protobuf validation libraries may adapt to this shape.
type Validator interface{ Validate() error }

// AuthorizationResolver maps a procedure to the permission and coarse resource
// required before a handler runs. Resource-specific checks based on request
// contents remain in the service handler.
type AuthorizationResolver interface {
	Resolve(context.Context, string) (auth.Permission, auth.Resource, error)
}

type AuthorizationResolverFunc func(context.Context, string) (auth.Permission, auth.Resource, error)

func (function AuthorizationResolverFunc) Resolve(ctx context.Context, procedure string) (auth.Permission, auth.Resource, error) {
	return function(ctx, procedure)
}

// PanicReport contains trusted diagnostic data. It must never be serialized to
// the RPC client.
type PanicReport struct {
	Procedure string
	Recovered any
	Stack     []byte
}

type PanicReporter interface {
	ReportPanic(context.Context, PanicReport)
}
type PanicReporterFunc func(context.Context, PanicReport)

func (function PanicReporterFunc) ReportPanic(ctx context.Context, report PanicReport) {
	function(ctx, report)
}
