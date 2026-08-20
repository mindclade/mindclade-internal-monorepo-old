// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package ingestion

import "go.mindclade.dev/libs/go/faults"

func invalid(reason, message string, cause error) error {
	options := []faults.Option{
		faults.WithReason(reason),
		faults.WithOperation("control.ingestion"),
		faults.WithRetryPolicy(faults.NoRetry()),
	}
	if cause == nil {
		return faults.New(faults.CodeInvalidArgument, message, options...)
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, message, append(options, faults.WithCause(cause))...)
}
