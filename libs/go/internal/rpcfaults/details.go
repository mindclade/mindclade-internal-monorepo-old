// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package rpcfaults

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/requestmeta"
)

const (
	MaximumMessageLength   = 2048
	MaximumMetadataEntries = 16
	MaximumMetadataValue   = 512
)

// Resource identifies an external-safe resource associated with a failure.
type Resource struct {
	Type string
	ID   string
}

// Details is the canonical wire-safe representation of a Mindclade fault.
type Details struct {
	Code      faults.Code
	Message   string
	Reason    string
	Operation string
	RequestID string
	TraceID   string
	Resource  Resource
	Retry     faults.RetryPolicy
	Metadata  map[string]string
}

// FromError extracts only information approved for transport serialization.
func FromError(ctx context.Context, err error) Details {
	if err == nil {
		return Details{}
	}

	fields := faults.FieldsOf(err)
	details := Details{
		Code:      faults.NormalizeCode(faults.CodeOf(err)),
		Message:   bounded(faults.PublicMessageOf(err), MaximumMessageLength),
		Reason:    bounded(faults.ReasonOf(err), MaximumMetadataValue),
		Operation: bounded(faults.OperationOf(err), MaximumMetadataValue),
		Retry:     faults.RetryPolicyOf(err).Normalized(),
		Metadata:  safeMetadata(fields),
	}
	details.RequestID = metadataValue(fields, faults.FieldRequestID)
	details.TraceID = metadataValue(fields, faults.FieldTraceID)
	details.Resource = Resource{
		Type: metadataValue(fields, faults.FieldResourceType),
		ID:   metadataValue(fields, faults.FieldResourceID),
	}

	if ctx != nil {
		if details.RequestID == "" {
			if requestID, ok := requestmeta.RequestIDFromContext(ctx); ok {
				details.RequestID = requestID.String()
			} else if requestID, ok := faults.RequestIDFromContext(ctx); ok {
				details.RequestID = bounded(requestID, MaximumMetadataValue)
			}
		}
		if details.TraceID == "" {
			if traceID, ok := faults.TraceIDFromContext(ctx); ok {
				details.TraceID = bounded(traceID, MaximumMetadataValue)
			}
		}
		if details.Operation == "" {
			if operation, ok := requestmeta.OperationFromContext(ctx); ok {
				details.Operation = operation.String()
			} else if operation, ok := faults.OperationFromContext(ctx); ok {
				details.Operation = bounded(operation, MaximumMetadataValue)
			}
		}
	}

	if details.Message == "" {
		details.Message = "request failed"
	}
	return details.Normalized()
}

// Normalized returns a bounded canonical value.
func (details Details) Normalized() Details {
	details.Code = faults.NormalizeCode(details.Code)
	details.Message = bounded(strings.TrimSpace(details.Message), MaximumMessageLength)
	details.Reason = bounded(strings.TrimSpace(details.Reason), MaximumMetadataValue)
	details.Operation = bounded(strings.TrimSpace(details.Operation), MaximumMetadataValue)
	details.RequestID = bounded(strings.TrimSpace(details.RequestID), MaximumMetadataValue)
	details.TraceID = bounded(strings.TrimSpace(details.TraceID), MaximumMetadataValue)
	details.Resource.Type = bounded(strings.TrimSpace(details.Resource.Type), MaximumMetadataValue)
	details.Resource.ID = bounded(strings.TrimSpace(details.Resource.ID), MaximumMetadataValue)
	details.Retry = details.Retry.Normalized()

	if len(details.Metadata) > 0 {
		normalized := make(map[string]string, min(len(details.Metadata), MaximumMetadataEntries))
		for _, key := range safeFieldKeys {
			if len(normalized) >= MaximumMetadataEntries {
				break
			}
			value := bounded(strings.TrimSpace(details.Metadata[key]), MaximumMetadataValue)
			if value != "" {
				normalized[key] = value
			}
		}
		if len(normalized) == 0 {
			details.Metadata = nil
		} else {
			details.Metadata = normalized
		}
	}
	return details
}

// ToError reconstructs a local transport-neutral fault from wire-safe details.
// It intentionally cannot reconstruct the server-side cause.
func ToError(details Details) error {
	details = details.Normalized()
	if details.Message == "" {
		details.Message = "request failed"
	}
	options := make([]faults.Option, 0, 6)
	if details.Reason != "" {
		options = append(options, faults.WithReason(details.Reason))
	}
	if details.Operation != "" {
		options = append(options, faults.WithOperation(details.Operation))
	}
	if details.RequestID != "" {
		options = append(options, faults.WithRequestID(details.RequestID))
	}
	if details.TraceID != "" {
		options = append(options, faults.WithTraceID(details.TraceID))
	}
	if details.Retry.Specified() {
		options = append(options, faults.WithRetryPolicy(details.Retry))
	}

	fields := faults.Fields{}
	for key, value := range details.Metadata {
		fields[key] = value
	}
	if details.Resource.Type != "" {
		fields[faults.FieldResourceType] = details.Resource.Type
	}
	if details.Resource.ID != "" {
		fields[faults.FieldResourceID] = details.Resource.ID
	}
	if len(fields) > 0 {
		options = append(options, faults.WithFields(fields))
	}
	return faults.New(details.Code, details.Message, options...)
}

// RetryAfter returns the explicit minimum delay carried by details.
func (details Details) RetryAfter() time.Duration {
	policy := details.Retry.Normalized()
	if policy.Kind != faults.RetryKindAfter {
		return 0
	}
	return policy.After
}

var safeFieldKeys = []string{
	faults.FieldTenantID,
	faults.FieldOrganizationID,
	faults.FieldResourceType,
	faults.FieldResourceID,
	faults.FieldModelID,
	faults.FieldRunID,
	"correlation_id",
	"causation_id",
}

func safeMetadata(fields faults.Fields) map[string]string {
	if len(fields) == 0 {
		return nil
	}
	metadata := make(map[string]string, len(safeFieldKeys))
	for _, key := range safeFieldKeys {
		if value := metadataValue(fields, key); value != "" {
			metadata[key] = value
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func metadataValue(fields faults.Fields, key string) string {
	value, ok := fields[key]
	if !ok || value == nil {
		return ""
	}
	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case fmt.Stringer:
		text = typed.String()
	case bool:
		text = strconv.FormatBool(typed)
	case int:
		text = strconv.Itoa(typed)
	case int64:
		text = strconv.FormatInt(typed, 10)
	case uint64:
		text = strconv.FormatUint(typed, 10)
	default:
		return ""
	}
	if text == faults.RedactedValue {
		return ""
	}
	return bounded(strings.TrimSpace(text), MaximumMetadataValue)
}

func bounded(value string, maximum int) string {
	if maximum <= 0 {
		return ""
	}
	value = strings.ToValidUTF8(value, "")
	if len(value) <= maximum {
		return value
	}
	end := maximum
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end]
}
