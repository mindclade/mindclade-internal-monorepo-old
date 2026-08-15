// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package interceptors

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"mindclade.internal/libs/go/auth"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/httpx"
	"strings"
)

type CredentialExtractor interface {
	ExtractCredential(metadata.MD) (auth.Credential, bool, error)
}
type CredentialExtractorFunc func(metadata.MD) (auth.Credential, bool, error)

func (function CredentialExtractorFunc) ExtractCredential(value metadata.MD) (auth.Credential, bool, error) {
	return function(value)
}

type AuthenticationConfig struct {
	Authenticator auth.Authenticator
	Extractor     CredentialExtractor
	Optional      bool
}

func UnaryAuthentication(config AuthenticationConfig) grpc.UnaryServerInterceptor {
	if nilInterface(config.Extractor) {
		config.Extractor = CredentialExtractorFunc(DefaultCredentialExtractor)
	}
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		derived, err := authenticate(ctx, config)
		if err != nil {
			return nil, err
		}
		return handler(derived, request)
	}
}
func StreamAuthentication(config AuthenticationConfig) grpc.StreamServerInterceptor {
	if nilInterface(config.Extractor) {
		config.Extractor = CredentialExtractorFunc(DefaultCredentialExtractor)
	}
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		derived, err := authenticate(stream.Context(), config)
		if err != nil {
			return err
		}
		return handler(server, &serverStreamWithContext{ServerStream: stream, ctx: derived})
	}
}
func authenticate(ctx context.Context, config AuthenticationConfig) (context.Context, error) {
	incoming, _ := metadata.FromIncomingContext(ctx)
	credential, present, err := config.Extractor.ExtractCredential(incoming)
	if err != nil {
		return ctx, err
	}
	if !present {
		if config.Optional {
			return ctx, nil
		}
		return ctx, faults.New(faults.CodeUnauthenticated, "authentication required", faults.WithReason("missing_credential"), faults.WithContextMetadata(ctx))
	}
	principal, err := auth.Authenticate(ctx, config.Authenticator, credential)
	if err != nil {
		return ctx, err
	}
	return auth.WithPrincipal(ctx, principal)
}
func DefaultCredentialExtractor(value metadata.MD) (auth.Credential, bool, error) {
	authorizationValues := nonEmptyValues(value.Get("authorization"))
	apiKeyValues := nonEmptyValues(value.Get(strings.ToLower(httpx.HeaderAPIKey)))
	if len(authorizationValues) > 1 || len(apiKeyValues) > 1 || len(authorizationValues) > 0 && len(apiKeyValues) > 0 {
		return auth.Credential{}, false, faults.New(faults.CodeUnauthenticated, "multiple credentials are not allowed", faults.WithReason("multiple_credentials"))
	}
	if len(apiKeyValues) == 1 {
		credential, err := auth.APIKey(apiKeyValues[0])
		return credential, true, err
	}
	if len(authorizationValues) == 0 {
		return auth.Credential{}, false, nil
	}
	fields := strings.Fields(authorizationValues[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || fields[1] == "" {
		return auth.Credential{}, false, faults.New(faults.CodeUnauthenticated, "invalid authorization metadata", faults.WithReason("invalid_authorization_metadata"))
	}
	credential, err := auth.Bearer(fields[1])
	return credential, true, err
}

func nonEmptyValues(values []string) []string {
	output := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			output = append(output, value)
		}
	}
	return output
}
