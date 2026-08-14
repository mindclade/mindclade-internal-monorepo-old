// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package rpcfaults contains implementation-only helpers shared by Mindclade's
// Connect and gRPC adapters.
//
// It deliberately models only information that is safe to place on an RPC
// wire. Wrapped causes, stacks, arbitrary fault fields, credentials, request
// bodies, and model inputs are never retained.
package rpcfaults
