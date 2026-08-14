// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package blob

import (
	"context"
	"errors"

	"mindclade.internal/libs/go/faults"
)

var (
	ErrInvalidKey      = errors.New("blob: invalid key")
	ErrInvalidMetadata = errors.New("blob: invalid metadata")
	ErrInvalidOptions  = errors.New("blob: invalid options")
	ErrInvalidObject   = errors.New("blob: invalid object")
	ErrObjectTooLarge  = errors.New("blob: object too large")
	ErrDigestMismatch  = errors.New("blob: digest mismatch")
	ErrPrecondition    = errors.New("blob: precondition failed")
	ErrUnsupported     = errors.New("blob: operation unsupported")
)

func invalidArgument(ctx context.Context, cause error, message, reason, operation string, fields faults.Fields) error {
	return faults.Wrap(cause, faults.CodeInvalidArgument, message,
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithFields(fields),
		faults.WithContextMetadata(ctx),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
