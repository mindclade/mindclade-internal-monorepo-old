// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package middleware

import (
	"net/http"

	"mindclade.internal/libs/go/auth"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/httpx"
)

type AuthorizationTarget struct {
	Permission auth.Permission
	Resource   auth.Resource
}

type AuthorizationResolver interface {
	ResolveAuthorization(*http.Request) (AuthorizationTarget, bool, error)
}
type AuthorizationResolverFunc func(*http.Request) (AuthorizationTarget, bool, error)

func (function AuthorizationResolverFunc) ResolveAuthorization(request *http.Request) (AuthorizationTarget, bool, error) {
	return function(request)
}

type AuthorizationConfig struct {
	Authorizer     auth.Authorizer
	Resolver       AuthorizationResolver
	RequireMapping bool
}

func Authorization(config AuthorizationConfig) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if nilInterface(config.Resolver) {
				if config.RequireMapping {
					err := faults.New(faults.CodePermissionDenied, "operation is not permitted", faults.WithReason("authorization_mapping_missing"), faults.WithContextMetadata(request.Context()))
					httpx.WriteError(request.Context(), writer, err, request.URL.Path)
					return
				}
				next.ServeHTTP(writer, request)
				return
			}
			target, mapped, err := config.Resolver.ResolveAuthorization(request)
			if err != nil {
				httpx.WriteError(request.Context(), writer, err, request.URL.Path)
				return
			}
			if !mapped {
				if config.RequireMapping {
					err := faults.New(faults.CodePermissionDenied, "operation is not permitted", faults.WithReason("authorization_mapping_missing"), faults.WithContextMetadata(request.Context()))
					httpx.WriteError(request.Context(), writer, err, request.URL.Path)
					return
				}
				next.ServeHTTP(writer, request)
				return
			}
			principal, err := auth.RequirePrincipal(request.Context())
			if err != nil {
				httpx.WriteError(request.Context(), writer, err, request.URL.Path)
				return
			}
			err = auth.Enforce(request.Context(), config.Authorizer, auth.AuthorizationRequest{
				Principal:  principal,
				Permission: target.Permission,
				Resource:   target.Resource,
			})
			if err != nil {
				httpx.WriteError(request.Context(), writer, err, request.URL.Path)
				return
			}
			next.ServeHTTP(writer, request)
		})
	}
}
