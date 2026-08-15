// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package grpcx

import (
	"context"
	"errors"
	"io"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/internal/rpcfaults"
)

const ErrorDomain = "mindclade.dev"

// StatusFromError converts an internal error into a client-safe gRPC status.
// Pre-existing status errors are re-encoded so only recognized standard details
// and bounded Mindclade metadata cross the transport boundary.
func StatusFromError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) {
		return err
	}
	if _, isFault := faults.AsFault(err); !isFault {
		if value, ok := grpcStatus(err); ok {
			return statusFromDetails(detailsFromStatus(value)).Err()
		}
	}
	return statusFromDetails(rpcfaults.FromError(ctx, err)).Err()
}

// ErrorFromStatus reconstructs a transport-neutral local fault from wire-safe
// gRPC status data. It cannot reconstruct the server's diagnostic cause.
func ErrorFromStatus(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) {
		return err
	}
	value, ok := grpcStatus(err)
	if !ok {
		code := faults.CodeOf(err)
		if code == faults.CodeUnknown {
			code = faults.CodeUnavailable
		}
		return faults.Wrap(
			err,
			code,
			faults.PublicMessageOf(err),
			faults.WithReason("grpc_transport_failure"),
			faults.WithOperation("grpcx.ErrorFromStatus"),
			faults.WithRetryPolicy(faults.BackoffRetry(0)),
		)
	}
	return rpcfaults.ToError(detailsFromStatus(value))
}

type grpcStatusProvider interface {
	GRPCStatus() *status.Status
}

// grpcStatus extracts the status object itself instead of using
// status.FromError, whose wrapped-error compatibility path replaces the wire
// message with the complete wrapper text. That wrapper may contain local
// diagnostics and must never become a public transport message.
func grpcStatus(err error) (*status.Status, bool) {
	var provider grpcStatusProvider
	if !errors.As(err, &provider) || provider == nil {
		return nil, false
	}
	value := provider.GRPCStatus()
	if value == nil {
		return nil, false
	}
	return value, true
}

func statusFromDetails(details rpcfaults.Details) *status.Status {
	details = details.Normalized()
	value := status.New(CodeFromFault(details.Code), details.Message)
	info := &errdetails.ErrorInfo{Reason: details.Reason, Domain: ErrorDomain, Metadata: errorMetadata(details)}
	if updated, err := value.WithDetails(info); err == nil {
		value = updated
	}
	if details.RequestID != "" {
		if updated, err := value.WithDetails(&errdetails.RequestInfo{RequestId: details.RequestID}); err == nil {
			value = updated
		}
	}
	if details.Resource.Type != "" || details.Resource.ID != "" {
		resource := &errdetails.ResourceInfo{ResourceType: details.Resource.Type, ResourceName: details.Resource.ID}
		if updated, err := value.WithDetails(resource); err == nil {
			value = updated
		}
	}
	if delay := details.RetryAfter(); delay > 0 {
		if updated, err := value.WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(delay)}); err == nil {
			value = updated
		}
	}
	return value
}

func detailsFromStatus(value *status.Status) rpcfaults.Details {
	if value == nil {
		return rpcfaults.Details{Code: faults.CodeUnknown, Message: "request failed", Retry: faults.NoRetry()}.Normalized()
	}
	details := rpcfaults.Details{Code: FaultCode(value.Code()), Message: value.Message()}
	for _, wire := range value.Details() {
		switch typed := wire.(type) {
		case *errdetails.ErrorInfo:
			if typed == nil || typed.Domain != ErrorDomain {
				continue
			}
			details.Reason = typed.Reason
			details.Operation = typed.Metadata["operation"]
			details.TraceID = typed.Metadata["trace_id"]
			details.Metadata = copyMetadata(typed.Metadata)
			if encoded := typed.Metadata["fault_code"]; encoded != "" {
				if parsed, err := faults.ParseCode(encoded); err == nil && CodeFromFault(parsed) == value.Code() {
					details.Code = parsed
				}
			}
			if attempts := parsePositiveInt(typed.Metadata["retry_max_attempts"]); attempts > 0 {
				details.Retry.MaxAttempts = attempts
			}
			if kind := faults.RetryKind(typed.Metadata["retry_kind"]); kind != "" {
				details.Retry.Kind = kind
			}
		case *errdetails.RequestInfo:
			if typed != nil {
				details.RequestID = typed.RequestId
			}
		case *errdetails.ResourceInfo:
			if typed != nil {
				details.Resource = rpcfaults.Resource{Type: typed.ResourceType, ID: typed.ResourceName}
			}
		case *errdetails.RetryInfo:
			if typed != nil && typed.RetryDelay != nil {
				if delay := typed.RetryDelay.AsDuration(); delay > 0 {
					details.Retry.Kind = faults.RetryKindAfter
					details.Retry.After = delay
				}
			}
		}
	}
	if !details.Retry.Specified() {
		details.Retry = faults.NoRetry()
	}
	return details.Normalized()
}

func errorMetadata(details rpcfaults.Details) map[string]string {
	metadata := copyMetadata(details.Metadata)
	if metadata == nil {
		metadata = map[string]string{}
	}
	if details.Operation != "" {
		metadata["operation"] = details.Operation
	}
	if details.TraceID != "" {
		metadata["trace_id"] = details.TraceID
	}
	metadata["fault_code"] = details.Code.String()
	policy := details.Retry.Normalized()
	if policy.Kind != faults.RetryKindUnspecified {
		metadata["retry_kind"] = string(policy.Kind)
	}
	if policy.MaxAttempts > 0 {
		metadata["retry_max_attempts"] = formatInt(policy.MaxAttempts)
	}
	return metadata
}

func copyMetadata(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	allowed := map[string]struct{}{
		faults.FieldTenantID:       {},
		faults.FieldOrganizationID: {},
		faults.FieldModelID:        {},
		faults.FieldRunID:          {},
		"correlation_id":           {},
		"causation_id":             {},
	}
	output := make(map[string]string, len(allowed))
	for key := range allowed {
		if value := input[key]; value != "" {
			output[key] = value
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
