// Copyright 2026 Mindclade. All rights reserved.
package routing

import "mindclade.internal/libs/go/faults"

func invalid(reason, message string, cause error) error {
	if cause == nil {
		return faults.New(faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.routing"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.routing"), faults.WithRetryPolicy(faults.NoRetry()))
}
