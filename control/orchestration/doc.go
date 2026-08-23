// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package orchestration owns durable workflow, stage, and attempt policy: graph
// compilation, dependency readiness, the stage and attempt state machines,
// cancellation propagation, fenced attempt ownership, and the reconciliation
// decision for one claimed unit of work.
//
// It owns no mechanism. Claim, heartbeat, retry, and dead-letter belong to
// coordination/workqueue; singleton authority to coordination/leadership; event
// publication to coordination/outbox; the reconcile loop to the Kubernetes
// controller runtime; and process lifecycle to servicekit. Nothing here starts a
// goroutine or opens a transaction — a repository implementation supplies the
// commit boundary, and services/control_plane constructs the providers.
//
// Run and job identity stay in control/runs; this package treats RunID and JobID
// as opaque canonical identifiers, exactly as WorkloadEnvelope already does.
// Kubernetes client mechanics live in the adapters, and scientific execution
// belongs to the Rust node runtime and the Python engines.
package orchestration
