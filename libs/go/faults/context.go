// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package faults

import (
	"context"
	"strings"
)

type metadataContextKey uint8

const (
	requestIDContextKey metadataContextKey = iota + 1
	traceIDContextKey
	operationContextKey
)

// ContextWithRequestID returns a child context carrying requestID. A blank
// identifier leaves ctx unchanged.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return contextWithMetadata(ctx, requestIDContextKey, requestID)
}

// RequestIDFromContext retrieves a request identifier.
func RequestIDFromContext(ctx context.Context) (string, bool) {
	return metadataFromContext(ctx, requestIDContextKey)
}

// ContextWithTraceID returns a child context carrying traceID. A blank
// identifier leaves ctx unchanged.
func ContextWithTraceID(ctx context.Context, traceID string) context.Context {
	return contextWithMetadata(ctx, traceIDContextKey, traceID)
}

// TraceIDFromContext retrieves a trace identifier.
func TraceIDFromContext(ctx context.Context) (string, bool) {
	return metadataFromContext(ctx, traceIDContextKey)
}

// ContextWithOperation returns a child context carrying a logical operation. A
// blank operation leaves ctx unchanged.
func ContextWithOperation(ctx context.Context, operation string) context.Context {
	return contextWithMetadata(ctx, operationContextKey, operation)
}

// OperationFromContext retrieves a logical operation.
func OperationFromContext(ctx context.Context) (string, bool) {
	return metadataFromContext(ctx, operationContextKey)
}

func contextWithMetadata(ctx context.Context, key metadataContextKey, value string) context.Context {
	if ctx == nil {
		panic("faults: nil context")
	}

	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return ctx
	}
	return context.WithValue(ctx, key, normalized)
}

func metadataFromContext(ctx context.Context, key metadataContextKey) (string, bool) {
	if ctx == nil {
		return "", false
	}

	value, ok := ctx.Value(key).(string)
	value = strings.TrimSpace(value)
	return value, ok && value != ""
}
