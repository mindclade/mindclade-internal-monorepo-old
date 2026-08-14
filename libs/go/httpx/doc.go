// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package httpx provides production HTTP server, client, request propagation,
// and error-envelope primitives for Mindclade services.
//
// It owns HTTP transport mechanics only. Authentication policy, business
// handlers, persistence, retries, and process termination remain outside this
// package. Connect and gRPC protocol semantics belong in connectx and grpcx.
package httpx
