// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package interceptors

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"go.mindclade.dev/libs/go/grpcx"
	"go.mindclade.dev/libs/go/requestmeta"
)

func UnaryRequestMetadata() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		derived, requestID, err := prepareServerContext(ctx, info.FullMethod)
		if err != nil {
			return nil, grpcx.StatusFromError(ctx, err)
		}
		_ = grpc.SetHeader(derived, metadata.Pairs(requestmeta.PropagationKeyRequestID, requestID.String()))
		return handler(derived, request)
	}
}
func StreamRequestMetadata() grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		derived, requestID, err := prepareServerContext(stream.Context(), info.FullMethod)
		if err != nil {
			return grpcx.StatusFromError(stream.Context(), err)
		}
		_ = stream.SetHeader(metadata.Pairs(requestmeta.PropagationKeyRequestID, requestID.String()))
		return handler(server, &serverStreamWithContext{ServerStream: stream, ctx: derived})
	}
}
func UnaryClientRequestMetadata() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, request, response any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, options ...grpc.CallOption) error {
		derived, err := prepareClientContext(ctx, method)
		if err != nil {
			return err
		}
		return invoker(derived, method, request, response, connection, options...)
	}
}
func StreamClientRequestMetadata() grpc.StreamClientInterceptor {
	return func(ctx context.Context, description *grpc.StreamDesc, connection *grpc.ClientConn, method string, streamer grpc.Streamer, options ...grpc.CallOption) (grpc.ClientStream, error) {
		derived, err := prepareClientContext(ctx, method)
		if err != nil {
			return nil, err
		}
		return streamer(derived, description, connection, method, options...)
	}
}
func prepareServerContext(ctx context.Context, fullMethod string) (context.Context, requestmeta.RequestID, error) {
	ctx, requestID, err := grpcx.ExtractIncoming(ctx)
	if err != nil {
		return ctx, requestID, err
	}
	method, err := grpcx.ParseMethod(fullMethod)
	if err != nil {
		return ctx, requestID, err
	}
	operation, err := method.Operation()
	if err != nil {
		return ctx, requestID, err
	}
	ctx, err = requestmeta.WithOperation(ctx, operation)
	return ctx, requestID, err
}
func prepareClientContext(ctx context.Context, fullMethod string) (context.Context, error) {
	ctx, _, err := requestmeta.EnsureRequestID(ctx)
	if err != nil {
		return ctx, err
	}
	method, err := grpcx.ParseMethod(fullMethod)
	if err != nil {
		return ctx, err
	}
	operation, err := method.Operation()
	if err != nil {
		return ctx, err
	}
	ctx, err = requestmeta.WithOperation(ctx, operation)
	if err != nil {
		return ctx, err
	}
	return grpcx.InjectOutgoing(ctx)
}
