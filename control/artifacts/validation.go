// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package artifacts

import "go.mindclade.dev/libs/go/faults"

func invalid(reason, message string, cause error) error {
	if cause == nil {
		return faults.New(faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.artifacts"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.artifacts"), faults.WithRetryPolicy(faults.NoRetry()))
}
func notFound(reason, message string) error {
	return faults.New(faults.CodeNotFound, message, faults.WithReason(reason), faults.WithOperation("control.artifacts"), faults.WithRetryPolicy(faults.NoRetry()))
}
