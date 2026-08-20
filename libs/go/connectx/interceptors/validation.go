// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package interceptors

import (
	"context"

	"connectrpc.com/connect"

	"go.mindclade.dev/libs/go/faults"
)

// Validation validates messages implementing Validator. Messages without a
// validator are passed through unchanged.
func Validation() connect.Interceptor { return validationInterceptor{} }

type validationInterceptor struct{}

func (validationInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		if request.Spec().IsClient {
			return next(ctx, request)
		}
		if err := validateMessage(request.Any()); err != nil {
			return nil, err
		}
		return next(ctx, request)
	}
}
func (validationInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}
func (validationInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, connection connect.StreamingHandlerConn) error {
		return next(ctx, &validatingHandlerConn{StreamingHandlerConn: connection, ctx: ctx})
	}
}

type validatingHandlerConn struct {
	connect.StreamingHandlerConn
	ctx context.Context
}

func (connection *validatingHandlerConn) Receive(message any) error {
	if err := connection.StreamingHandlerConn.Receive(message); err != nil {
		return err
	}
	return validateMessage(message)
}

func validateMessage(message any) error {
	validator, ok := message.(Validator)
	if !ok || nilInterface(validator) {
		return nil
	}
	if err := validator.Validate(); err != nil {
		return faults.Wrap(err, faults.CodeInvalidArgument, "invalid RPC request", faults.WithReason("rpc_request_validation_failed"))
	}
	return nil
}
