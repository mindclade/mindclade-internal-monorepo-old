// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package retry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mindclade.dev/libs/go/faults"
)

var (
	ErrInvalidPolicy        = errors.New("retry: invalid policy")
	ErrInvalidBackoff       = errors.New("retry: invalid backoff")
	ErrInvalidJitter        = errors.New("retry: invalid jitter")
	ErrInvalidOperationName = errors.New("retry: invalid operation name")
	ErrNilContext           = errors.New("retry: nil context")
	ErrNilOperation         = errors.New("retry: nil operation")
	ErrNilExecutor          = errors.New("retry: nil executor")
	ErrNilClock             = errors.New("retry: nil clock")
	ErrNilClassifier        = errors.New("retry: nil classifier")
	ErrNilRandomSource      = errors.New("retry: nil random source")
	ErrExhausted            = errors.New("retry: attempts exhausted")
	ErrInterrupted          = errors.New("retry: interrupted")
)

const (
	operationNewExecutor = "retry.NewExecutor"
	operationExecute     = "retry.Executor.Do"
)

func invalidArgument(cause error, message, reason, operation string, fields faults.Fields) error {
	options := []faults.Option{
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()),
	}
	if len(fields) > 0 {
		options = append(options, faults.WithFields(fields))
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, message, options...)
}

func exhaustedError(ctx context.Context, operation string, attempts int, elapsed time.Duration, lastErr error) error {
	code := faults.CodeOf(lastErr)
	if code == faults.CodeUnknown || code == faults.CodeCanceled || code == faults.CodeDeadlineExceeded {
		code = faults.CodeUnavailable
	}
	fields := faults.Fields{
		"retry_operation": operation,
		"attempts":        attempts,
		"elapsed":         elapsed.String(),
	}
	return faults.Wrap(
		errors.Join(ErrExhausted, lastErr),
		code,
		"retry attempts exhausted",
		faults.WithReason("retry_exhausted"),
		faults.WithOperation(operationExecute),
		faults.WithFields(fields),
		faults.WithContextMetadata(ctx),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

func interruptedError(ctx context.Context, operation string, attempts int, elapsed time.Duration, cause, lastErr error) error {
	if cause == nil {
		cause = context.Canceled
	}
	code := faults.CodeOf(cause)
	if code != faults.CodeDeadlineExceeded {
		code = faults.CodeCanceled
	}
	message := "retry operation canceled"
	reason := "retry_canceled"
	if code == faults.CodeDeadlineExceeded {
		message = "retry operation deadline exceeded"
		reason = "retry_deadline_exceeded"
	}
	fields := faults.Fields{
		"retry_operation": operation,
		"attempts":        attempts,
		"elapsed":         elapsed.String(),
	}
	return faults.Wrap(
		errors.Join(ErrInterrupted, cause, lastErr),
		code,
		message,
		faults.WithReason(reason),
		faults.WithOperation(operationExecute),
		faults.WithFields(fields),
		faults.WithContextMetadata(ctx),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

func invalidPolicyField(name string, value any, cause error) error {
	return invalidArgument(
		errors.Join(ErrInvalidPolicy, cause),
		"invalid retry policy",
		"invalid_retry_policy",
		"retry.NewPolicy",
		faults.Fields{"field": name, "value": fmt.Sprint(value)},
	)
}
