// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package servicekit provides transport-neutral lifecycle primitives for
// long-running Mindclade Go services.
//
// A Service starts components in registration order, supervises their run
// loops, stops them in reverse registration order, exposes deterministic
// liveness and readiness reports, and applies bounded startup and shutdown
// budgets driven by an injectable Layer 0 clock. The package also provides
// signal-aware execution, build metadata,
// lifecycle events, panic containment at extension boundaries, and immutable
// lifecycle snapshots.
//
// All exported operational failures use go.mindclade.dev/libs/go/faults for
// stable codes, reasons, operations, retry intent, and diagnostic fields while
// retaining errors.Is compatibility with servicekit sentinel errors.
//
// The package deliberately does not own configuration parsing, dependency
// injection, HTTP or gRPC servers, logging, tracing, metrics, authentication,
// storage, Kubernetes clients, business logic, or process termination. Those
// concerns integrate through components, probes, observers, and adapters.
package servicekit
