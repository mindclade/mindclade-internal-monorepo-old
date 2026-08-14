// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package outbox exposes the stable storage-facing transactional-outbox API.
//
// The coordination state machine is implemented by
// libs/go/coordination/outbox. This package intentionally provides a thin,
// source-compatible façade so storage-oriented consumers can depend on the
// original blueprint path without duplicating claims, fencing, retry, or
// dispatch semantics.
//
// New code that owns a dispatcher or workflow loop should normally import the
// coordination package directly. Repository and transaction adapters may use
// this package when the storage namespace communicates ownership more clearly.
package outbox
