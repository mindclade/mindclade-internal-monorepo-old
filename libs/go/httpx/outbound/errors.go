// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package outbound

import (
	"errors"

	"mindclade.internal/libs/go/faults"
)

var (
	ErrInvalidPolicy     = errors.New("httpx/outbound: invalid policy")
	ErrURLRejected       = errors.New("httpx/outbound: URL rejected")
	ErrHostNotAllowed    = errors.New("httpx/outbound: host not allowed")
	ErrAddressNotAllowed = errors.New("httpx/outbound: address not allowed")
	ErrResponseTooLarge  = errors.New("httpx/outbound: response too large")
	ErrMediaTypeRejected = errors.New("httpx/outbound: media type rejected")
	ErrEncodingRejected  = errors.New("httpx/outbound: content encoding rejected")
	ErrResolutionFailed  = errors.New("httpx/outbound: name resolution failed")
)

func reject(cause error, reason, operation string, fields faults.Fields) error {
	if cause == nil {
		cause = ErrURLRejected
	}
	return faults.Wrap(cause, faults.CodePermissionDenied, "outbound HTTP request rejected",
		faults.WithReason(reason), faults.WithOperation(operation), faults.WithFields(fields), faults.WithRetryPolicy(faults.NoRetry()))
}

func invalid(cause error, reason string) error {
	if cause == nil {
		cause = ErrInvalidPolicy
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, "invalid outbound HTTP policy",
		faults.WithReason(reason), faults.WithOperation("httpx.outbound.NewClient"), faults.WithRetryPolicy(faults.NoRetry()))
}
