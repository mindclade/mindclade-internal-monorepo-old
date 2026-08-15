// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package idempotency defines transport-neutral replay protection for
// Mindclade write operations.
//
// Callers bind an external idempotency Key to a service-owned Scope and a
// canonical request Fingerprint. Store implementations atomically acquire a
// lease, return an existing completed result for a matching replay, report an
// in-progress execution, or reject reuse with a different fingerprint.
//
// The core package stores opaque successful results. HTTP, Connect, and gRPC
// adapters decide how to encode transport responses. Durable Store adapters
// belong with their storage technology and must preserve the state-machine and
// compare-and-swap semantics defined here.
package idempotency
