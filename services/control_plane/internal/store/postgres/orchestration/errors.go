// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestrationpostgres

import (
	"context"

	"go.mindclade.dev/libs/go/faults"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
)

// provider preserves the SQLSTATE classification storage/sql/postgres derives.
// Re-coding it here would erase the difference between a serialization failure
// that must be retried and a constraint violation that never can be.
func provider(ctx context.Context, err error, operation string) error {
	qualified := sqlpostgres.Qualify(ctx, err, operation)
	return faults.Wrap(qualified, faults.CodeOf(qualified), "orchestration store operation failed",
		faults.WithReason(valueOr(faults.ReasonOf(qualified), "orchestration_store_failed")),
		faults.WithOperation(operation), faults.WithFields(faults.FieldsOf(qualified)),
		faults.WithRetryPolicy(faults.RetryPolicyOf(qualified)), faults.WithContextMetadata(ctx))
}

func internal(ctx context.Context, err error, operation, reason string) error {
	return faults.Wrap(err, faults.CodeInternal, "orchestration store invariant failed",
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

// domainError raises the fault the domain's own reference adapter would raise.
// The reason strings are the reference adapter's, verbatim, so a caller that
// switches on faults.IsReason keeps working when a factory swaps the memory
// adapter for this one.
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
