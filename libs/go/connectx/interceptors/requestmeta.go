// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package interceptors

import (
	"context"
	"net/http"

	"connectrpc.com/connect"

	"mindclade.internal/libs/go/connectx"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/httpx"
	"mindclade.internal/libs/go/requestmeta"
)

// RequestMetadata establishes request lineage on handlers and injects existing
// lineage on clients. It supports unary and streaming RPCs.
func RequestMetadata() connect.Interceptor { return requestMetadataInterceptor{} }

type requestMetadataInterceptor struct{}

func (requestMetadataInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		if request.Spec().IsClient {
			derived, err := prepareClientMetadata(ctx, request.Spec().Procedure)
			if err != nil {
				return nil, err
			}
			if err := requestmeta.Inject(derived, httpx.HeaderCarrier{Header: request.Header()}); err != nil {
				return nil, err
			}
			return next(derived, request)
		}
		ctx, err := establishMetadata(ctx, request.Header(), request.Spec().Procedure)
		if err != nil {
			return nil, connectx.EncodeError(ctx, err)
		}
		response, err := next(ctx, request)
		if err == nil && response != nil {
			_ = requestmeta.Inject(ctx, httpx.HeaderCarrier{Header: response.Header()})
		}
		return response, err
	}
}

func (requestMetadataInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		derived, err := prepareClientMetadata(ctx, spec.Procedure)
		if err != nil {
			return &failedStreamingClientConn{err: err}
		}
		connection := next(derived, spec)
		if connection == nil {
			return connection
		}
		if err := requestmeta.Inject(derived, httpx.HeaderCarrier{Header: connection.RequestHeader()}); err != nil {
			return &failedStreamingClientConn{StreamingClientConn: connection, err: err}
		}
		return connection
	}
}

func (requestMetadataInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, connection connect.StreamingHandlerConn) error {
		ctx, err := establishMetadata(ctx, connection.RequestHeader(), connection.Spec().Procedure)
		if err != nil {
			return connectx.EncodeError(ctx, err)
		}
		_ = requestmeta.Inject(ctx, httpx.HeaderCarrier{Header: connection.ResponseHeader()})
		return next(ctx, connection)
	}
}

func prepareClientMetadata(ctx context.Context, procedure string) (context.Context, error) {
	ctx, _, err := requestmeta.EnsureRequestID(ctx)
	if err != nil {
		return ctx, err
	}
	operation, err := connectx.OperationForProcedure(procedure)
	if err != nil {
		return ctx, faults.Wrap(err, faults.CodeInvalidArgument, "invalid RPC procedure", faults.WithReason("invalid_rpc_procedure"))
	}
	return requestmeta.WithOperation(ctx, operation)
}

func establishMetadata(ctx context.Context, header http.Header, procedure string) (context.Context, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := httpx.ValidateRequestMetadataHeaders(header); err != nil {
		return nil, err
	}
	ctx, _, err := requestmeta.ExtractOrGenerate(ctx, httpx.HeaderCarrier{Header: header})
	if err != nil {
		return nil, err
	}
	operation, err := connectx.OperationForProcedure(procedure)
	if err != nil {
		return nil, faults.Wrap(err, faults.CodeInvalidArgument, "invalid RPC procedure", faults.WithReason("invalid_rpc_procedure"))
	}
	return requestmeta.WithOperation(ctx, operation)
}

type failedStreamingClientConn struct {
	connect.StreamingClientConn
	err error
}

func (connection *failedStreamingClientConn) Spec() connect.Spec {
	if connection != nil && connection.StreamingClientConn != nil {
		return connection.StreamingClientConn.Spec()
	}
	return connect.Spec{}
}
func (connection *failedStreamingClientConn) Peer() connect.Peer {
	if connection != nil && connection.StreamingClientConn != nil {
		return connection.StreamingClientConn.Peer()
	}
	return connect.Peer{}
}
func (connection *failedStreamingClientConn) RequestHeader() http.Header {
	if connection != nil && connection.StreamingClientConn != nil {
		return connection.StreamingClientConn.RequestHeader()
	}
	return make(http.Header)
}
func (connection *failedStreamingClientConn) ResponseHeader() http.Header {
	if connection != nil && connection.StreamingClientConn != nil {
		return connection.StreamingClientConn.ResponseHeader()
	}
	return make(http.Header)
}
func (connection *failedStreamingClientConn) ResponseTrailer() http.Header {
	if connection != nil && connection.StreamingClientConn != nil {
		return connection.StreamingClientConn.ResponseTrailer()
	}
	return make(http.Header)
}
func (connection *failedStreamingClientConn) Send(any) error       { return connection.err }
func (connection *failedStreamingClientConn) Receive(any) error    { return connection.err }
func (connection *failedStreamingClientConn) CloseRequest() error  { return connection.err }
func (connection *failedStreamingClientConn) CloseResponse() error { return connection.err }
