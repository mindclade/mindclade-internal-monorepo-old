// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package middleware

import (
	"net/http"
	"strings"

	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/httpx"
)

type AuthenticationConfig struct {
	Authenticator auth.Authenticator
	Optional      bool
	APIKeyHeader  string
	// BearerTokenHeader accepts a raw bearer token from one exact trusted
	// reverse-proxy header (for example, IAP). When set, Authorization and API
	// key credentials remain mutually exclusive with it.
	BearerTokenHeader string
}

func Authentication(config AuthenticationConfig) Middleware {
	if config.APIKeyHeader == "" {
		config.APIKeyHeader = httpx.HeaderAPIKey
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			credential, present, err := extractCredential(request, config.APIKeyHeader, config.BearerTokenHeader)
			if err != nil {
				writer.Header().Set("WWW-Authenticate", "Bearer")
				httpx.WriteError(request.Context(), writer, err, request.URL.Path)
				return
			}
			if !present {
				if config.Optional {
					next.ServeHTTP(writer, request)
					return
				}
				writer.Header().Set("WWW-Authenticate", "Bearer")
				err := faults.New(faults.CodeUnauthenticated, "authentication required", faults.WithReason("missing_credential"), faults.WithContextMetadata(request.Context()))
				httpx.WriteError(request.Context(), writer, err, request.URL.Path)
				return
			}
			principal, err := auth.Authenticate(request.Context(), config.Authenticator, credential)
			if err != nil {
				writer.Header().Set("WWW-Authenticate", "Bearer")
				httpx.WriteError(request.Context(), writer, err, request.URL.Path)
				return
			}
			ctx, err := auth.WithPrincipal(request.Context(), principal)
			if err != nil {
				httpx.WriteError(request.Context(), writer, err, request.URL.Path)
				return
			}
			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}
}

func extractCredential(request *http.Request, apiKeyHeader, bearerTokenHeader string) (auth.Credential, bool, error) {
	authorizationValues := nonEmptyHeaderValues(request.Header.Values("Authorization"))
	apiKeyValues := nonEmptyHeaderValues(request.Header.Values(apiKeyHeader))
	proxyBearerValues := []string(nil)
	if bearerTokenHeader != "" {
		proxyBearerValues = nonEmptyHeaderValues(request.Header.Values(bearerTokenHeader))
	}
	presentedKinds := 0
	for _, values := range [][]string{authorizationValues, apiKeyValues, proxyBearerValues} {
		if len(values) > 0 {
			presentedKinds++
		}
	}
	if len(authorizationValues) > 1 || len(apiKeyValues) > 1 || len(proxyBearerValues) > 1 || presentedKinds > 1 {
		return auth.Credential{}, false, faults.New(faults.CodeUnauthenticated, "multiple credentials are not allowed", faults.WithReason("multiple_credentials"))
	}
	if len(proxyBearerValues) == 1 {
		credential, err := auth.Bearer(proxyBearerValues[0])
		return credential, err == nil, err
	}
	if len(apiKeyValues) == 1 {
		credential, err := auth.APIKey(apiKeyValues[0])
		return credential, err == nil, err
	}
	if len(authorizationValues) == 0 {
		return auth.Credential{}, false, nil
	}
	parts := strings.Fields(authorizationValues[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return auth.Credential{}, false, faults.New(faults.CodeUnauthenticated, "invalid authorization header", faults.WithReason("invalid_authorization_header"))
	}
	credential, err := auth.Bearer(parts[1])
	return credential, err == nil, err
}

func nonEmptyHeaderValues(values []string) []string {
	output := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			output = append(output, value)
		}
	}
	return output
}
