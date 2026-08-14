// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package interceptors

import (
	"context"
	"google.golang.org/grpc"
	"mindclade.internal/libs/go/auth"
	"mindclade.internal/libs/go/faults"
)

type AuthorizationTarget struct {
	Permission auth.Permission
	Resource   auth.Resource
}
type AuthorizationResolver interface {
	ResolveAuthorization(string, any) (AuthorizationTarget, bool, error)
}
type AuthorizationResolverFunc func(string, any) (AuthorizationTarget, bool, error)

func (function AuthorizationResolverFunc) ResolveAuthorization(method string, request any) (AuthorizationTarget, bool, error) {
	return function(method, request)
}

type AuthorizationConfig struct {
	Authorizer     auth.Authorizer
	Resolver       AuthorizationResolver
	RequireMapping bool
}

func UnaryAuthorization(config AuthorizationConfig) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := authorize(ctx, config, info.FullMethod, request); err != nil {
			return nil, err
		}
		return handler(ctx, request)
	}
}
func StreamAuthorization(config AuthorizationConfig) grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := authorize(stream.Context(), config, info.FullMethod, nil); err != nil {
			return err
		}
		return handler(server, stream)
	}
}
func authorize(ctx context.Context, config AuthorizationConfig, method string, request any) error {
	if nilInterface(config.Resolver) {
		if config.RequireMapping {
			return faults.New(faults.CodePermissionDenied, "operation is not permitted", faults.WithReason("authorization_mapping_missing"), faults.WithContextMetadata(ctx))
		}
		return nil
	}
	target, mapped, err := config.Resolver.ResolveAuthorization(method, request)
	if err != nil {
		return err
	}
	if !mapped {
		if config.RequireMapping {
			return faults.New(faults.CodePermissionDenied, "operation is not permitted", faults.WithReason("authorization_mapping_missing"), faults.WithContextMetadata(ctx))
		}
		return nil
	}
	principal, err := auth.RequirePrincipal(ctx)
	if err != nil {
		return err
	}
	return auth.Enforce(ctx, config.Authorizer, auth.AuthorizationRequest{Principal: principal, Permission: target.Permission, Resource: target.Resource})
}
