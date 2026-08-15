// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package middleware

import (
	"net/http"
	"strings"

	"mindclade.internal/libs/go/httpx"
	"mindclade.internal/libs/go/requestmeta"
)

type OperationResolver interface {
	ResolveOperation(*http.Request) (requestmeta.Operation, error)
}
type OperationResolverFunc func(*http.Request) (requestmeta.Operation, error)

func (function OperationResolverFunc) ResolveOperation(request *http.Request) (requestmeta.Operation, error) {
	return function(request)
}

// RequestMetadata extracts validated inbound request lineage, guarantees a
// request ID, sets a local logical operation, and returns lineage headers.
func RequestMetadata(resolver OperationResolver) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if err := httpx.ValidateRequestMetadataHeaders(request.Header); err != nil {
				httpx.WriteError(request.Context(), writer, err, request.URL.Path)
				return
			}
			ctx, requestID, err := requestmeta.ExtractOrGenerate(request.Context(), httpx.HeaderCarrier{Header: request.Header})
			if err != nil {
				httpx.WriteError(request.Context(), writer, err, request.URL.Path)
				return
			}
			operation := requestmeta.MustParseOperation("http." + strings.ToLower(request.Method))
			if resolver != nil {
				operation, err = resolver.ResolveOperation(request)
				if err != nil {
					httpx.WriteError(ctx, writer, err, request.URL.Path)
					return
				}
			}
			ctx, err = requestmeta.WithOperation(ctx, operation)
			if err != nil {
				httpx.WriteError(ctx, writer, err, request.URL.Path)
				return
			}
			writer.Header().Set(httpx.HeaderRequestID, requestID.String())
			if metadata, ok := requestmeta.FromContext(ctx); ok {
				if !metadata.CorrelationID.IsZero() {
					writer.Header().Set(httpx.HeaderCorrelationID, metadata.CorrelationID.String())
				}
			}
			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}
}
