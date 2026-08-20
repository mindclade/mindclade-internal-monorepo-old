// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package connectx

import (
	"strconv"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/internal/rpcfaults"
)

// ErrorDomain identifies Mindclade-owned google.rpc.ErrorInfo details.
const ErrorDomain = "mindclade.dev"

func addStandardDetails(encoded *connect.Error, details rpcfaults.Details) {
	if encoded == nil {
		return
	}
	details = details.Normalized()
	addConnectDetail(encoded, &errdetails.ErrorInfo{
		Reason:   details.Reason,
		Domain:   ErrorDomain,
		Metadata: standardMetadata(details),
	})
	if details.RequestID != "" {
		addConnectDetail(encoded, &errdetails.RequestInfo{RequestId: details.RequestID})
	}
	if details.Resource.Type != "" || details.Resource.ID != "" {
		addConnectDetail(encoded, &errdetails.ResourceInfo{
			ResourceType: details.Resource.Type,
			ResourceName: details.Resource.ID,
		})
	}
	if delay := details.RetryAfter(); delay > 0 {
		addConnectDetail(encoded, &errdetails.RetryInfo{RetryDelay: durationpb.New(delay)})
	}
}

func addConnectDetail(encoded *connect.Error, message proto.Message) {
	detail, err := connect.NewErrorDetail(message)
	if err == nil {
		encoded.AddDetail(detail)
	}
}

func mergeStandardDetails(details *rpcfaults.Details, encoded *connect.Error) {
	if details == nil || encoded == nil {
		return
	}
	for _, detail := range encoded.Details() {
		if detail == nil {
			continue
		}
		value, err := detail.Value()
		if err != nil {
			continue
		}
		switch typed := value.(type) {
		case *errdetails.ErrorInfo:
			if typed.Domain != ErrorDomain {
				continue
			}
			if encodedCode, err := faults.ParseCode(typed.Metadata["fault_code"]); err == nil && CodeFromFault(encodedCode) == encoded.Code() {
				details.Code = encodedCode
			}
			details.Reason = typed.Reason
			details.Operation = typed.Metadata["operation"]
			details.TraceID = typed.Metadata["trace_id"]
			details.Metadata = safeStandardMetadata(typed.Metadata)
			details.Retry = retryFromStandardMetadata(typed.Metadata, details.Retry)
		case *errdetails.RequestInfo:
			details.RequestID = typed.RequestId
		case *errdetails.ResourceInfo:
			details.Resource = rpcfaults.Resource{Type: typed.ResourceType, ID: typed.ResourceName}
		case *errdetails.RetryInfo:
			if typed.RetryDelay != nil {
				if delay := typed.RetryDelay.AsDuration(); delay > 0 {
					details.Retry = faults.DelayedRetry(delay, details.Retry.MaxAttempts)
				}
			}
		}
	}
}

func standardMetadata(details rpcfaults.Details) map[string]string {
	metadata := make(map[string]string, len(details.Metadata)+5)
	for key, value := range details.Metadata {
		metadata[key] = value
	}
	if details.Operation != "" {
		metadata["operation"] = details.Operation
	}
	if details.TraceID != "" {
		metadata["trace_id"] = details.TraceID
	}
	policy := details.Retry.Normalized()
	if policy.Specified() {
		metadata["retry_kind"] = string(policy.Kind)
	}
	if policy.MaxAttempts > 0 {
		metadata["retry_max_attempts"] = strconv.Itoa(policy.MaxAttempts)
	}
	metadata["fault_code"] = details.Code.String()
	return metadata
}

func safeStandardMetadata(input map[string]string) map[string]string {
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

func retryFromStandardMetadata(metadata map[string]string, fallback faults.RetryPolicy) faults.RetryPolicy {
	policy := fallback.Normalized()
	if kind := faults.RetryKind(metadata["retry_kind"]); kind != "" {
		policy.Kind = kind
	}
	if value := metadata["retry_max_attempts"]; value != "" {
		if attempts, err := strconv.Atoi(value); err == nil && attempts > 0 {
			policy.MaxAttempts = attempts
		}
	}
	return policy.Normalized()
}
