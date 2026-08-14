// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package interceptors

import (
	"context"

	"connectrpc.com/connect"

	"mindclade.internal/libs/go/auth"
	"mindclade.internal/libs/go/faults"
)

type AuthorizationConfig struct {
	Authorizer auth.Authorizer
	Resolver   AuthorizationResolver
}

func Authorization(config AuthorizationConfig) connect.Interceptor {
	return authorizationInterceptor{config: config}
}

type authorizationInterceptor struct{ config AuthorizationConfig }

func (interceptor authorizationInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		if request.Spec().IsClient {
			return next(ctx, request)
		}
		if err := interceptor.authorize(ctx, request.Spec().Procedure); err != nil {
			return nil, err
		}
		return next(ctx, request)
	}
}

func (interceptor authorizationInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}
func (interceptor authorizationInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, connection connect.StreamingHandlerConn) error {
		if err := interceptor.authorize(ctx, connection.Spec().Procedure); err != nil {
			return err
		}
		return next(ctx, connection)
	}
}

func (interceptor authorizationInterceptor) authorize(ctx context.Context, procedure string) error {
	if nilInterface(interceptor.config.Resolver) {
		return faults.New(faults.CodeFailedPrecondition, "authorization is not configured", faults.WithReason("authorization_resolver_missing"), faults.WithContextMetadata(ctx))
	}
	principal, err := auth.RequirePrincipal(ctx)
	if err != nil {
		return err
	}
	permission, resource, err := interceptor.config.Resolver.Resolve(ctx, procedure)
	if err != nil {
		return err
	}
	return auth.Enforce(ctx, interceptor.config.Authorizer, auth.AuthorizationRequest{Principal: principal, Permission: permission, Resource: resource})
}
