// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package signing

import (
	"context"
	"errors"

	"go.mindclade.dev/libs/go/faults"
)

func invalid(cause error, message, reason, operation string, fields faults.Fields) error {
	return faults.Wrap(cause, faults.CodeInvalidArgument, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithFields(fields), faults.WithRetryPolicy(faults.NoRetry()))
}
func verifyFault(cause error, reason, operation string, keyID KeyID) error {
	return faults.Wrap(cause, faults.CodeUnauthenticated, "signature verification failed",
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithField("key_id", keyID.String()), faults.WithRetryPolicy(faults.NoRetry()))
}
func checkContext(ctx context.Context, operation string) error {
	if ctx == nil {
		return faults.New(faults.CodeInvalidArgument, "signing context is required", faults.WithReason("nil_context"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := ctx.Err(); err != nil {
		code := faults.CodeCanceled
		reason := "signing_operation_canceled"
		if errors.Is(err, context.DeadlineExceeded) {
			code = faults.CodeDeadlineExceeded
			reason = "signing_operation_deadline_exceeded"
		}
		return faults.Wrap(err, code, "signing operation did not complete", faults.WithReason(reason), faults.WithOperation(operation), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}
