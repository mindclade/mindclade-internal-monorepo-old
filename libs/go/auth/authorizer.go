// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package auth

import (
	"context"

	"mindclade.internal/libs/go/faults"
)

type AuthorizationRequest struct {
	Principal  Principal
	Permission Permission
	Resource   Resource
}

func (request AuthorizationRequest) Validate() error {
	if err := request.Principal.Validate(); err != nil {
		return err
	}
	if !request.Permission.Valid() || request.Permission.String() == "*" || request.Permission.String()[len(request.Permission.String())-1] == '*' {
		return newFault(ErrInvalidPermission, faults.CodeInvalidArgument, "invalid requested permission", "invalid_requested_permission", "auth.AuthorizationRequest.Validate", faults.Fields{"permission": request.Permission.String()})
	}
	if err := request.Resource.Validate(); err != nil {
		return err
	}
	return nil
}

type Authorizer interface {
	Authorize(context.Context, AuthorizationRequest) (Decision, error)
}

type AuthorizerFunc func(context.Context, AuthorizationRequest) (Decision, error)

func (function AuthorizerFunc) Authorize(ctx context.Context, request AuthorizationRequest) (Decision, error) {
	return function(ctx, request)
}

// Enforce returns nil only for a valid allow decision.
func Enforce(ctx context.Context, authorizer Authorizer, request AuthorizationRequest) error {
	if ctx == nil {
		return newFault(ErrNilContext, faults.CodeInvalidArgument, "nil authorization context", "nil_context", "auth.Enforce", nil)
	}
	if nilInterface(authorizer) {
		return newFault(ErrNilAuthorizer, faults.CodeFailedPrecondition, "authorization is not configured", "authorizer_missing", "auth.Enforce", nil)
	}
	if err := request.Validate(); err != nil {
		return err
	}
	decision, err := authorizer.Authorize(ctx, request)
	if err != nil {
		return preserveFault(err, faults.PublicMessageOf(err), "auth.Enforce")
	}
	if err := decision.Validate(); err != nil {
		return err
	}
	if decision.Allowed() {
		return nil
	}
	reason := decision.Reason()
	if reason == "" {
		reason = "authorization_denied"
	}
	return newFault(
		ErrAuthorizationDenied,
		faults.CodePermissionDenied,
		"operation is not permitted",
		reason,
		"auth.Enforce",
		faults.Fields{
			"principal_kind":         request.Principal.Kind().String(),
			"permission":             request.Permission.String(),
			faults.FieldResourceType: request.Resource.Type().String(),
			faults.FieldResourceID:   request.Resource.ID().String(),
			"policy_id":              decision.PolicyID(),
		},
	)
}

// PermissionAuthorizer authorizes from the immutable grants on Principal and
// enforces tenant and organization scope. Only an explicitly system-scoped
// principal may operate globally when a target declares a scope.
type PermissionAuthorizer struct{}

func (PermissionAuthorizer) Authorize(_ context.Context, request AuthorizationRequest) (Decision, error) {
	if err := request.Validate(); err != nil {
		return Decision{}, err
	}
	if !scopeMatches(request.Principal.Kind(), request.Principal.OrganizationID(), request.Resource.OrganizationID()) {
		return Deny("organization_scope_mismatch"), nil
	}
	if !scopeMatches(request.Principal.Kind(), request.Principal.TenantID(), request.Resource.TenantID()) {
		return Deny("tenant_scope_mismatch"), nil
	}
	if !request.Principal.Allows(request.Permission) {
		return Deny("permission_not_granted"), nil
	}
	return Allow("permission_granted"), nil
}

func scopeMatches(kind PrincipalKind, principalScope, resourceScope interface {
	IsZero() bool
	String() string
}) bool {
	if resourceScope.IsZero() {
		return true
	}
	if principalScope.IsZero() {
		return kind == PrincipalKindSystem
	}
	return principalScope.String() == resourceScope.String()
}
