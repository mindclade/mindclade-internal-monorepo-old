// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package cache

import "mindclade.internal/libs/go/faults"

func invalidEntry(message, reason string) error {
	return faults.Wrap(ErrInvalidEntry, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("cache.Entry.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
}
func invalidOptions(message, reason string) error {
	return faults.Wrap(ErrInvalidOptions, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("cache.Options.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
}
