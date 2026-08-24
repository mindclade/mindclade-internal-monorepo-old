// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package schedulingpostgres

import (
	"context"

	"go.mindclade.dev/libs/go/faults"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
)

// provider preserves the SQLSTATE classification storage/sql/postgres derives.
// Re-coding it here would erase the difference between a serialization failure
// that must be retried and a constraint violation that never can be -- and this
// package generates far more of the former than orchestration does, because
// every mutation contends on one ledger row.
func provider(ctx context.Context, err error, operation string) error {
	qualified := sqlpostgres.Qualify(ctx, err, operation)
	return faults.Wrap(qualified, faults.CodeOf(qualified), "scheduling store operation failed",
		faults.WithReason(valueOr(faults.ReasonOf(qualified), "scheduling_store_failed")),
		faults.WithOperation(operation), faults.WithFields(faults.FieldsOf(qualified)),
		faults.WithRetryPolicy(faults.RetryPolicyOf(qualified)), faults.WithContextMetadata(ctx))
}

func internal(ctx context.Context, err error, operation, reason string) error {
	return faults.Wrap(err, faults.CodeInternal, "scheduling store invariant failed",
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

// domainError raises the fault the domain's own reference adapter would raise.
// The reason strings and the public messages are scheduling.MemoryRepository's,
// verbatim, so a caller that switches on faults.IsReason keeps working when a
// factory swaps the memory adapter for this one. Where this package raises a
// fault the memory adapter has no counterpart for -- a read bound, a corrupt
// ledger row -- the reason carries no domain prefix and is named at its call
// site as adapter-local.
func domainError(ctx context.Context, code faults.Code, reason, message, operation string) error {
	return faults.New(code, message, faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

func valueOr(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

// domainWrap is domainError with a cause preserved. The reference adapter wraps
// the underlying validation failure into several of its faults, and dropping it
// would cost an operator the one line that says which field was wrong.
func domainWrap(ctx context.Context, cause error, code faults.Code, reason, message, operation string) error {
	if cause == nil {
		return domainError(ctx, code, reason, message, operation)
	}
	return faults.Wrap(cause, code, message, faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
