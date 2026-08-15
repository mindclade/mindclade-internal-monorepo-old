// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package middleware

import (
	"net/http"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/httpx"
)

// MaximumBody rejects declared bodies larger than maximumBytes and protects
// handlers from reading beyond the limit through http.MaxBytesReader.
func MaximumBody(maximumBytes int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if maximumBytes <= 0 {
				err := faults.New(faults.CodeInternal, "HTTP body limit is not configured", faults.WithReason("invalid_body_limit"))
				httpx.WriteError(request.Context(), writer, err, request.URL.Path)
				return
			}
			if request.ContentLength > maximumBytes {
				err := faults.New(faults.CodeResourceExhausted, "request body is too large", faults.WithReason("request_body_too_large"), faults.WithContextMetadata(request.Context()))
				httpx.WriteError(request.Context(), writer, err, request.URL.Path)
				return
			}
			if request.Body != nil {
				request.Body = http.MaxBytesReader(writer, request.Body, maximumBytes)
			}
			next.ServeHTTP(writer, request)
		})
	}
}
