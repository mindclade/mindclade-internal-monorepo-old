// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package otel

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc/stats"
)

// NewServerStatsHandler constructs the official OpenTelemetry gRPC server
// stats handler for grpcx.ServerConfig.
func NewServerStatsHandler(options ...otelgrpc.Option) stats.Handler {
	return otelgrpc.NewServerHandler(options...)
}

// NewClientStatsHandler constructs the official OpenTelemetry gRPC client
// stats handler for grpcx.ClientConfig.
func NewClientStatsHandler(options ...otelgrpc.Option) stats.Handler {
	return otelgrpc.NewClientHandler(options...)
}
