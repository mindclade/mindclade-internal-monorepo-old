// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package auth

import (
	"context"
	"errors"

	"mindclade.internal/libs/go/faults"
)

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) (context.Context, error) {
	if ctx == nil {
		return nil, newFault(ErrNilContext, faults.CodeInvalidArgument, "nil authentication context", "nil_context", "auth.WithPrincipal", nil)
	}
	if err := principal.Validate(); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, principalContextKey{}, principal), nil
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	if !ok || principal.Validate() != nil {
		return Principal{}, false
	}
	return principal, true
}

func RequirePrincipal(ctx context.Context) (Principal, error) {
	if ctx == nil {
		return Principal{}, newFault(ErrNilContext, faults.CodeInvalidArgument, "nil authentication context", "nil_context", "auth.RequirePrincipal", nil)
	}
	if principal, ok := PrincipalFromContext(ctx); ok {
		return principal, nil
	}
	return Principal{}, newFault(errors.Join(ErrUnauthenticated, ErrInvalidPrincipal), faults.CodeUnauthenticated, "authentication required", "principal_missing", "auth.RequirePrincipal", nil)
}
