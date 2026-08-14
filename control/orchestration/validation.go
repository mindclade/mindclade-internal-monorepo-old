// Copyright 2026 Mindclade. All rights reserved.
package orchestration

import "mindclade.internal/libs/go/faults"

func invalid(reason, message string, cause error) error {
	if cause == nil {
		return faults.New(faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.orchestration"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.orchestration"), faults.WithRetryPolicy(faults.NoRetry()))
}
