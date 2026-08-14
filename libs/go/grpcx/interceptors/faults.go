// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package interceptors

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"mindclade.internal/libs/go/grpcx"
)

func UnaryFaultTranslation() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		response, err := handler(ctx, request)
		return response, grpcx.StatusFromError(ctx, err)
	}
}
func StreamFaultTranslation() grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return grpcx.StatusFromError(stream.Context(), handler(server, stream))
	}
}
func UnaryClientFaultTranslation() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, request, response any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, options ...grpc.CallOption) error {
		return grpcx.ErrorFromStatus(invoker(ctx, method, request, response, connection, options...))
	}
}
func StreamClientFaultTranslation() grpc.StreamClientInterceptor {
	return func(ctx context.Context, description *grpc.StreamDesc, connection *grpc.ClientConn, method string, streamer grpc.Streamer, options ...grpc.CallOption) (grpc.ClientStream, error) {
		stream, err := streamer(ctx, description, connection, method, options...)
		if err != nil {
			return nil, grpcx.ErrorFromStatus(err)
		}
		return &decodingClientStream{ClientStream: stream}, nil
	}
}

type decodingClientStream struct{ grpc.ClientStream }

func (stream *decodingClientStream) SendMsg(message any) error {
	return grpcx.ErrorFromStatus(stream.ClientStream.SendMsg(message))
}
func (stream *decodingClientStream) RecvMsg(message any) error {
	return grpcx.ErrorFromStatus(stream.ClientStream.RecvMsg(message))
}
func (stream *decodingClientStream) CloseSend() error {
	return grpcx.ErrorFromStatus(stream.ClientStream.CloseSend())
}
func (stream *decodingClientStream) Header() (metadata.MD, error) {
	header, err := stream.ClientStream.Header()
	return header, grpcx.ErrorFromStatus(err)
}
