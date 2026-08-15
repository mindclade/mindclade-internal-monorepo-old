// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package grpcx adapts Mindclade's transport-neutral service contracts to
// native grpc-go.
//
// Keep this package only for consumers that require grpc-go APIs, resolvers,
// balancers, credentials, or third-party native gRPC integrations. Connect
// remains the preferred application RPC server surface.
package grpcx
