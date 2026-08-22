// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

// Package iapauth authenticates the separately isolated administrative HTTP
// surface with Google Cloud IAP assertions.
package iapauth

import (
	"context"
	"strings"
	"time"

	"google.golang.org/api/idtoken"

	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/services/control_plane/internal/config"
)

const (
	HeaderName               = "x-goog-iap-jwt-assertion"
	Issuer                   = "https://cloud.google.com/iap"
	maximumAssertionBytes    = 16 << 10
	maximumAssertionLifetime = 15 * time.Minute
)

type tokenValidator interface {
	Validate(context.Context, string, string) (*idtoken.Payload, error)
}

type authenticator struct {
	audience    string
	validator   tokenValidator
	clock       clock.Clock
	permissions auth.PermissionSet
}

func NewAuthenticator(ctx context.Context, settings config.Settings, value clock.Clock) (auth.Authenticator, error) {
	if ctx == nil || value == nil {
		return nil, misconfigured("iap_authenticator_dependencies_missing")
	}
	audience := strings.TrimSpace(settings.AuthIAPAudience)
	if audience == "" || len(audience) > 2048 {
		return nil, misconfigured("iap_audience_not_configured")
	}
	validator, err := idtoken.NewValidator(ctx)
	if err != nil {
		return nil, misconfigured("iap_validator_unavailable")
	}
	permissions, err := policyPermissions()
	if err != nil {
		return nil, err
	}
	return &authenticator{audience: audience, validator: validator, clock: value, permissions: permissions}, nil
}

func (value *authenticator) Authenticate(ctx context.Context, credential auth.Credential) (auth.Principal, error) {
	if ctx == nil || value == nil || value.validator == nil || value.clock == nil {
		return auth.Principal{}, unauthenticated("iap_authenticator_unavailable")
	}
	if credential.Scheme() != auth.CredentialSchemeBearer {
		return auth.Principal{}, unauthenticated("iap_credential_scheme_invalid")
	}
	raw := credential.Value()
	if len(raw) == 0 || len(raw) > maximumAssertionBytes {
		return auth.Principal{}, unauthenticated("iap_assertion_size_invalid")
	}
	payload, err := value.validator.Validate(ctx, string(raw), value.audience)
	if err != nil || payload == nil {
		return auth.Principal{}, unauthenticated("iap_assertion_invalid")
	}
	if payload.Issuer != Issuer {
		return auth.Principal{}, unauthenticated("iap_issuer_invalid")
	}
	if payload.Audience != value.audience {
		return auth.Principal{}, unauthenticated("iap_audience_invalid")
	}
	now := value.clock.Now().Round(0).UTC()
	issuedAt := time.Unix(payload.IssuedAt, 0).UTC()
	expiresAt := time.Unix(payload.Expires, 0).UTC()
	if payload.IssuedAt <= 0 || issuedAt.After(now) || !expiresAt.After(now) || !expiresAt.After(issuedAt) || expiresAt.Sub(issuedAt) > maximumAssertionLifetime {
		return auth.Principal{}, unauthenticated("iap_lifetime_invalid")
	}
	subject := strings.TrimSpace(payload.Subject)
	if !strings.HasPrefix(subject, "accounts.google.com:") {
		return auth.Principal{}, unauthenticated("iap_subject_invalid")
	}
	principal, err := auth.NewPrincipal(auth.PrincipalKindUser, subject,
		auth.WithIssuer(Issuer), auth.WithPermissions(value.permissions),
		auth.WithAuthenticationTimes(issuedAt, expiresAt))
	if err != nil {
		return auth.Principal{}, unauthenticated("iap_subject_invalid")
	}
	return principal, nil
}

func policyPermissions() (auth.PermissionSet, error) {
	values := []string{
		"ai_gateway.policies.read",
		"ai_gateway.policy_proposals.create",
		"ai_gateway.policy_proposals.read",
		"ai_gateway.policy_proposals.approve",
		"ai_gateway.policy_proposals.reject",
		"ai_gateway.policy_proposals.cancel",
	}
	permissions := make([]auth.Permission, 0, len(values))
	for _, item := range values {
		permission, err := auth.ParsePermission(item)
		if err != nil {
			return auth.PermissionSet{}, err
		}
		permissions = append(permissions, permission)
	}
	return auth.NewPermissionSet(permissions...)
}

func unauthenticated(reason string) error {
	return faults.New(faults.CodeUnauthenticated, "IAP authentication failed",
		faults.WithReason(reason), faults.WithOperation("controlplane.iapauth.Authenticate"),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func misconfigured(reason string) error {
	return faults.New(faults.CodeFailedPrecondition, "control-plane IAP authentication is not configured",
		faults.WithReason(reason), faults.WithOperation("controlplane.iapauth.NewAuthenticator"),
		faults.WithRetryPolicy(faults.NoRetry()))
}
