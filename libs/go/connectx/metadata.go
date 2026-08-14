// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package connectx

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/internal/rpcfaults"
)

const (
	HeaderErrorCode        = "Mindclade-Error-Code"
	HeaderErrorReason      = "Mindclade-Error-Reason"
	HeaderErrorOperation   = "Mindclade-Error-Operation"
	HeaderRequestID        = "Mindclade-Request-Id"
	HeaderTraceID          = "Mindclade-Trace-Id"
	HeaderResourceType     = "Mindclade-Resource-Type"
	HeaderResourceID       = "Mindclade-Resource-Id"
	HeaderRetryKind        = "Mindclade-Retry-Kind"
	HeaderRetryAfterMillis = "Mindclade-Retry-After-Ms"
	HeaderRetryMaxAttempts = "Mindclade-Retry-Max-Attempts"
)

func injectDetails(header http.Header, details rpcfaults.Details) {
	if header == nil {
		return
	}
	details = details.Normalized()
	setHeader(header, HeaderErrorCode, details.Code.String())
	setHeader(header, HeaderErrorReason, details.Reason)
	setHeader(header, HeaderErrorOperation, details.Operation)
	setHeader(header, HeaderRequestID, details.RequestID)
	setHeader(header, HeaderTraceID, details.TraceID)
	setHeader(header, HeaderResourceType, details.Resource.Type)
	setHeader(header, HeaderResourceID, details.Resource.ID)
	policy := details.Retry.Normalized()
	if policy.Specified() {
		setHeader(header, HeaderRetryKind, string(policy.Kind))
	}
	if policy.After > 0 {
		setHeader(header, HeaderRetryAfterMillis, strconv.FormatInt(policy.After.Milliseconds(), 10))
	}
	if policy.MaxAttempts > 0 {
		setHeader(header, HeaderRetryMaxAttempts, strconv.Itoa(policy.MaxAttempts))
	}
	for key, value := range details.Metadata {
		switch key {
		case "correlation_id":
			setHeader(header, "Mindclade-Correlation-Id", value)
		case "causation_id":
			setHeader(header, "Mindclade-Causation-Id", value)
		}
	}
}

func extractDetails(header http.Header, code faults.Code, message string) rpcfaults.Details {
	details := rpcfaults.Details{
		Code:      code,
		Message:   message,
		Reason:    headerValue(header, HeaderErrorReason),
		Operation: headerValue(header, HeaderErrorOperation),
		RequestID: headerValue(header, HeaderRequestID),
		TraceID:   headerValue(header, HeaderTraceID),
		Resource: rpcfaults.Resource{
			Type: headerValue(header, HeaderResourceType),
			ID:   headerValue(header, HeaderResourceID),
		},
	}
	if encoded := headerValue(header, HeaderErrorCode); encoded != "" {
		if parsed, err := faults.ParseCode(encoded); err == nil && CodeFromFault(parsed) == CodeFromFault(code) {
			details.Code = parsed
		}
	}
	details.Retry = retryPolicyFromHeader(header)
	metadata := map[string]string{}
	if value := headerValue(header, "Mindclade-Correlation-Id"); value != "" {
		metadata["correlation_id"] = value
	}
	if value := headerValue(header, "Mindclade-Causation-Id"); value != "" {
		metadata["causation_id"] = value
	}
	if len(metadata) > 0 {
		details.Metadata = metadata
	}
	return details.Normalized()
}

func retryPolicyFromHeader(header http.Header) faults.RetryPolicy {
	kind := faults.RetryKind(strings.TrimSpace(headerValue(header, HeaderRetryKind)))
	var after time.Duration
	if value := headerValue(header, HeaderRetryAfterMillis); value != "" {
		if millis, err := strconv.ParseInt(value, 10, 64); err == nil && millis > 0 {
			after = time.Duration(millis) * time.Millisecond
		}
	}
	var attempts int
	if value := headerValue(header, HeaderRetryMaxAttempts); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			attempts = parsed
		}
	}
	return faults.RetryPolicy{Kind: kind, After: after, MaxAttempts: attempts}.Normalized()
}

func setHeader(header http.Header, key, value string) {
	value = boundedHeaderValue(value)
	if value != "" {
		header.Set(key, value)
	}
}

func headerValue(header http.Header, key string) string {
	if header == nil {
		return ""
	}
	return boundedHeaderValue(header.Get(key))
}

func boundedHeaderValue(value string) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "")
	value = strings.Map(func(character rune) rune {
		if character == '\r' || character == '\n' || character == 0 {
			return -1
		}
		return character
	}, value)
	if len(value) <= rpcfaults.MaximumMetadataValue {
		return value
	}
	end := rpcfaults.MaximumMetadataValue
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end]
}
