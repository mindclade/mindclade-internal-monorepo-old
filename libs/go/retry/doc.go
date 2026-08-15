// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package retry executes bounded, context-aware retry policies.
//
// The package is deliberately separate from faults. The faults package records
// retry intent on an error; retry decides whether, when, and how another
// attempt is executed. By default, only errors carrying an explicit retryable
// faults.RetryPolicy are retried.
//
// Executors are safe for concurrent use. Policies are immutable after
// construction. Observers are best-effort diagnostics: observer panics are
// recovered and never change operation semantics.
package retry
