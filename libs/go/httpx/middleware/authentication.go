// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package middleware

import (
	"net/http"
	"strings"

	"mindclade.internal/libs/go/auth"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/httpx"
)

type AuthenticationConfig struct {
	Authenticator auth.Authenticator
	Optional      bool
	APIKeyHeader  string
}

func Authentication(config AuthenticationConfig) Middleware {
	if config.APIKeyHeader == "" {
		config.APIKeyHeader = httpx.HeaderAPIKey
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			credential, present, err := extractCredential(request, config.APIKeyHeader)
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

func extractCredential(request *http.Request, apiKeyHeader string) (auth.Credential, bool, error) {
	authorizationValues := nonEmptyHeaderValues(request.Header.Values("Authorization"))
	apiKeyValues := nonEmptyHeaderValues(request.Header.Values(apiKeyHeader))
	if len(authorizationValues) > 1 || len(apiKeyValues) > 1 || len(authorizationValues) > 0 && len(apiKeyValues) > 0 {
		return auth.Credential{}, false, faults.New(faults.CodeUnauthenticated, "multiple credentials are not allowed", faults.WithReason("multiple_credentials"))
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
