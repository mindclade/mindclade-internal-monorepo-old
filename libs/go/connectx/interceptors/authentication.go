// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package interceptors

import (
	"context"
	"net/http"
	"strings"

	"connectrpc.com/connect"

	"mindclade.internal/libs/go/auth"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/httpx"
)

type AuthenticationConfig struct {
	Authenticator auth.Authenticator
	Optional      bool
	APIKeyHeader  string
}

func Authentication(config AuthenticationConfig) connect.Interceptor {
	if strings.TrimSpace(config.APIKeyHeader) == "" {
		config.APIKeyHeader = httpx.HeaderAPIKey
	}
	return authenticationInterceptor{config: config}
}

type authenticationInterceptor struct{ config AuthenticationConfig }

func (interceptor authenticationInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		if request.Spec().IsClient {
			return next(ctx, request)
		}
		ctx, err := interceptor.authenticate(ctx, request.Header())
		if err != nil {
			return nil, err
		}
		return next(ctx, request)
	}
}

func (interceptor authenticationInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}
func (interceptor authenticationInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, connection connect.StreamingHandlerConn) error {
		ctx, err := interceptor.authenticate(ctx, connection.RequestHeader())
		if err != nil {
			return err
		}
		return next(ctx, connection)
	}
}

func (interceptor authenticationInterceptor) authenticate(ctx context.Context, header http.Header) (context.Context, error) {
	credential, present, err := credentialFromHeader(header, interceptor.config.APIKeyHeader)
	if err != nil {
		return nil, err
	}
	if !present {
		if interceptor.config.Optional {
			return ctx, nil
		}
		return nil, faults.New(faults.CodeUnauthenticated, "authentication required", faults.WithReason("missing_credential"), faults.WithContextMetadata(ctx))
	}
	principal, err := auth.Authenticate(ctx, interceptor.config.Authenticator, credential)
	if err != nil {
		return nil, err
	}
	return auth.WithPrincipal(ctx, principal)
}

func credentialFromHeader(header http.Header, apiKeyHeader string) (auth.Credential, bool, error) {
	authorizationValues := nonEmptyHeaderValues(header.Values("Authorization"))
	apiKeyValues := nonEmptyHeaderValues(header.Values(apiKeyHeader))
	if len(authorizationValues) > 1 || len(apiKeyValues) > 1 || len(authorizationValues) > 0 && len(apiKeyValues) > 0 {
		return auth.Credential{}, false, faults.New(faults.CodeUnauthenticated, "multiple authentication credentials supplied", faults.WithReason("multiple_credentials"))
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
