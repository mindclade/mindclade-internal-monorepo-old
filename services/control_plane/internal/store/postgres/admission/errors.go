// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admissionpostgres

import (
	"context"

	"go.mindclade.dev/libs/go/faults"
	sqlpostgres "go.mindclade.dev/libs/go/storage/sql/postgres"
)

func provider(ctx context.Context, err error, operation string) error {
	qualified := sqlpostgres.Qualify(ctx, err, operation)
	return faults.Wrap(qualified, faults.CodeOf(qualified), "admission store operation failed",
		faults.WithReason(valueOr(faults.ReasonOf(qualified), "admission_store_failed")),
		faults.WithOperation(operation), faults.WithFields(faults.FieldsOf(qualified)),
		faults.WithRetryPolicy(faults.RetryPolicyOf(qualified)), faults.WithContextMetadata(ctx))
}

func internal(ctx context.Context, err error, operation, reason string) error {
	return faults.Wrap(err, faults.CodeInternal, "admission store invariant failed",
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

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
