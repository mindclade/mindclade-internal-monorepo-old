// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package requestmeta

import (
	"context"
	"errors"

	"go.mindclade.dev/libs/go/faults"
)

type contextKey struct{}

// WithMetadata merges metadata into any existing request metadata and returns
// a child context. It also populates the legacy faults context bridge so
// faults.WithContextMetadata continues to work during migration.
func WithMetadata(ctx context.Context, metadata Metadata) (context.Context, error) {
	if ctx == nil {
		return nil, invalidArgument(ErrNilContext, "nil request context", "nil_context", nil)
	}
	if err := metadata.Validate(); err != nil {
		return nil, err
	}
	if existing, ok := FromContext(ctx); ok {
		metadata = existing.Merge(metadata)
	}
	metadata = metadata.WithDefaultCorrelation()
	if metadata.IsZero() {
		return ctx, nil
	}
	ctx = context.WithValue(ctx, contextKey{}, metadata)
	if !metadata.RequestID.IsZero() {
		ctx = faults.ContextWithRequestID(ctx, metadata.RequestID.String())
	}
	if !metadata.Operation.IsZero() {
		ctx = faults.ContextWithOperation(ctx, metadata.Operation.String())
	}
	return ctx, nil
}

// FromContext retrieves request metadata.
func FromContext(ctx context.Context) (Metadata, bool) {
	if ctx == nil {
		return Metadata{}, false
	}
	metadata, ok := ctx.Value(contextKey{}).(Metadata)
	if !ok || metadata.IsZero() || metadata.Validate() != nil {
		return Metadata{}, false
	}
	return metadata, true
}

// EnsureRequestID returns a context with a request ID, generating one when
// necessary. The correlation ID defaults to that request ID.
func EnsureRequestID(ctx context.Context) (context.Context, RequestID, error) {
	if ctx == nil {
		return nil, RequestID{}, invalidArgument(ErrNilContext, "nil request context", "nil_context", nil)
	}
	if metadata, ok := FromContext(ctx); ok && !metadata.RequestID.IsZero() {
		return ctx, metadata.RequestID, nil
	}
	requestID, err := NewRequestID()
	if err != nil {
		return nil, RequestID{}, err
	}
	// Add only the generated request ID. WithMetadata preserves any inbound
	// correlation or causation lineage and supplies request-based correlation
	// only when the context does not already carry one.
	ctx, err = WithMetadata(ctx, Metadata{RequestID: requestID})
	return ctx, requestID, err
}

func WithRequestID(ctx context.Context, requestID RequestID) (context.Context, error) {
	return WithMetadata(ctx, Metadata{RequestID: requestID})
}

func WithCorrelationID(ctx context.Context, correlationID CorrelationID) (context.Context, error) {
	return WithMetadata(ctx, Metadata{CorrelationID: correlationID})
}

func WithCausationID(ctx context.Context, causationID CausationID) (context.Context, error) {
	return WithMetadata(ctx, Metadata{CausationID: causationID})
}

func WithOperation(ctx context.Context, operation Operation) (context.Context, error) {
	return WithMetadata(ctx, Metadata{Operation: operation})
}

func RequestIDFromContext(ctx context.Context) (RequestID, bool) {
	metadata, ok := FromContext(ctx)
	return metadata.RequestID, ok && !metadata.RequestID.IsZero()
}

func CorrelationIDFromContext(ctx context.Context) (CorrelationID, bool) {
	metadata, ok := FromContext(ctx)
	return metadata.CorrelationID, ok && !metadata.CorrelationID.IsZero()
}

func CausationIDFromContext(ctx context.Context) (CausationID, bool) {
	metadata, ok := FromContext(ctx)
	return metadata.CausationID, ok && !metadata.CausationID.IsZero()
}

func OperationFromContext(ctx context.Context) (Operation, bool) {
	metadata, ok := FromContext(ctx)
	return metadata.Operation, ok && !metadata.Operation.IsZero()
}

// RequireRequestID returns the context request ID or a failed-precondition
// fault when the boundary failed to establish one.
func RequireRequestID(ctx context.Context) (RequestID, error) {
	if ctx == nil {
		return RequestID{}, invalidArgument(ErrNilContext, "nil request context", "nil_context", nil)
	}
	if requestID, ok := RequestIDFromContext(ctx); ok {
		return requestID, nil
	}
	return RequestID{}, faults.Wrap(
		errors.Join(ErrInvalidMetadata, ErrInvalidRequestID),
		faults.CodeFailedPrecondition,
		"request identifier is not established",
		faults.WithReason("request_id_missing"),
		faults.WithOperation("requestmeta.RequireRequestID"),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
