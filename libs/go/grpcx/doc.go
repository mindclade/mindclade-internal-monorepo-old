// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package grpcx adapts Mindclade's transport-neutral service contracts to
// native grpc-go.
//
// Keep this package only for consumers that require grpc-go APIs, resolvers,
// balancers, credentials, or third-party native gRPC integrations. Connect
// remains the preferred application RPC server surface.
package grpcx
