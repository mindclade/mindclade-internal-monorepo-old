// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package connectx

import (
	"context"
	"errors"
	"io"

	"connectrpc.com/connect"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/internal/rpcfaults"
)

// EncodeError converts err into a client-safe Connect error. Existing Connect
// errors are treated as explicitly transport-safe, but their metadata is
// reduced to Mindclade's bounded allowlist.
func EncodeError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var existing *connect.Error
	if errors.As(err, &existing) && existing != nil {
		details := extractDetails(existing.Meta(), FaultCode(existing.Code()), existing.Message())
		mergeStandardDetails(&details, existing)
		details = details.Normalized()
		encoded := connect.NewError(existing.Code(), errors.New(details.Message))
		addStandardDetails(encoded, details)
		injectDetails(encoded.Meta(), details)
		return encoded
	}
	details := rpcfaults.FromError(ctx, err)
	encoded := connect.NewError(CodeFromFault(details.Code), errors.New(details.Message))
	addStandardDetails(encoded, details)
	injectDetails(encoded.Meta(), details)
	return encoded
}

// DecodeError converts a Connect error received by a client into a local
// transport-neutral fault. It never invents the server's diagnostic cause.
func DecodeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) {
		return err
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) || connectErr == nil {
		code := faults.CodeOf(err)
		retry := faults.RetryPolicyOf(err)
		if code == faults.CodeUnknown {
			code = faults.CodeUnavailable
		}
		if !retry.Specified() && code == faults.CodeUnavailable {
			retry = faults.BackoffRetry(0)
		}
		return faults.Wrap(
			err,
			code,
			faults.PublicMessageOf(err),
			faults.WithReason("connect_transport_failure"),
			faults.WithOperation("connectx.DecodeError"),
			faults.WithRetryPolicy(retry),
		)
	}
	details := extractDetails(connectErr.Meta(), FaultCode(connectErr.Code()), connectErr.Message())
	mergeStandardDetails(&details, connectErr)
	if details.Code == faults.CodeUnknown && !connect.IsWireError(err) {
		details.Code = faults.CodeUnavailable
		if details.Reason == "" {
			details.Reason = "connect_transport_failure"
		}
		if !details.Retry.Specified() {
			details.Retry = faults.BackoffRetry(0)
		}
	}
	return rpcfaults.ToError(details)
}
