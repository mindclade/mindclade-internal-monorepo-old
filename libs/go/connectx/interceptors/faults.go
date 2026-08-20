// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package interceptors

import (
	"context"

	"connectrpc.com/connect"

	"go.mindclade.dev/libs/go/connectx"
)

// FaultTranslation encodes handler errors and decodes client errors for unary
// and streaming calls.
func FaultTranslation() connect.Interceptor { return faultTranslationInterceptor{} }

type faultTranslationInterceptor struct{}

func (faultTranslationInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		response, err := next(ctx, request)
		if request.Spec().IsClient {
			return response, connectx.DecodeError(err)
		}
		return response, connectx.EncodeError(ctx, err)
	}
}

func (faultTranslationInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		connection := next(ctx, spec)
		if connection == nil {
			return nil
		}
		return &decodingClientConn{StreamingClientConn: connection}
	}
}

func (faultTranslationInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, connection connect.StreamingHandlerConn) error {
		return connectx.EncodeError(ctx, next(ctx, connection))
	}
}

type decodingClientConn struct{ connect.StreamingClientConn }

func (connection *decodingClientConn) Send(message any) error {
	return connectx.DecodeError(connection.StreamingClientConn.Send(message))
}
func (connection *decodingClientConn) Receive(message any) error {
	return connectx.DecodeError(connection.StreamingClientConn.Receive(message))
}
func (connection *decodingClientConn) CloseRequest() error {
	return connectx.DecodeError(connection.StreamingClientConn.CloseRequest())
}
func (connection *decodingClientConn) CloseResponse() error {
	return connectx.DecodeError(connection.StreamingClientConn.CloseResponse())
}
