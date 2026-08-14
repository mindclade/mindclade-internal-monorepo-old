// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package requestmeta

import (
	"errors"

	"mindclade.internal/libs/go/faults"
)

var (
	ErrInvalidRequestID   = errors.New("requestmeta: invalid request ID")
	ErrInvalidCorrelation = errors.New("requestmeta: invalid correlation ID")
	ErrInvalidCausation   = errors.New("requestmeta: invalid causation ID")
	ErrInvalidOperation   = errors.New("requestmeta: invalid operation")
	ErrInvalidMetadata    = errors.New("requestmeta: invalid metadata")
	ErrNilContext         = errors.New("requestmeta: nil context")
	ErrNilCarrier         = errors.New("requestmeta: nil carrier")
)

func invalidArgument(cause error, message, reason string, fields faults.Fields) error {
	return faults.Wrap(
		cause,
		faults.CodeInvalidArgument,
		message,
		faults.WithReason(reason),
		faults.WithOperation("requestmeta.validate"),
		faults.WithFields(fields),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
