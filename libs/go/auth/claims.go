// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package auth

import (
	"errors"
	"strings"
	"time"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

const MaximumAudiences = 32

// Claims is a verified identity-claim snapshot. Signature verification and
// provider-specific claim decoding happen before constructing this value.
type Claims struct {
	Issuer         string
	Subject        string
	Audience       []string
	IssuedAt       time.Time
	NotBefore      time.Time
	ExpiresAt      time.Time
	TokenID        string
	PrincipalID    identifiers.ID
	OrganizationID identifiers.ID
	TenantID       identifiers.ID
	Permissions    []Permission
	Attributes     map[string]string
}

func (claims Claims) Clone() Claims {
	claims.Audience = append([]string(nil), claims.Audience...)
	claims.Permissions = append([]Permission(nil), claims.Permissions...)
	claims.Attributes = cloneAttributes(claims.Attributes)
	return claims
}

// Validate verifies claim shape and lifetime at now. skew is applied
// symmetrically to not-before and expiration checks.
func (claims Claims) Validate(now time.Time, skew time.Duration) error {
	if skew < 0 {
		skew = 0
	}
	claims.Issuer = strings.TrimSpace(claims.Issuer)
	claims.Subject = strings.TrimSpace(claims.Subject)
	if !validIdentityText(claims.Issuer, 512) || !validIdentityText(claims.Subject, 512) || claims.ExpiresAt.IsZero() {
		return newFault(ErrInvalidClaims, faults.CodeUnauthenticated, "invalid authentication claims", "invalid_claims", "auth.Claims.Validate", nil)
	}
	if !claims.NotBefore.IsZero() && now.Add(skew).Before(claims.NotBefore) {
		return newFault(errors.Join(ErrInvalidClaims, ErrUnauthenticated), faults.CodeUnauthenticated, "authentication is not yet valid", "claims_not_yet_valid", "auth.Claims.Validate", nil)
	}
	if !now.Before(claims.ExpiresAt.Add(skew)) {
		return newFault(errors.Join(ErrInvalidClaims, ErrUnauthenticated), faults.CodeUnauthenticated, "authentication has expired", "claims_expired", "auth.Claims.Validate", nil)
	}
	if !claims.IssuedAt.IsZero() && claims.IssuedAt.After(now.Add(skew)) {
		return newFault(ErrInvalidClaims, faults.CodeUnauthenticated, "invalid authentication claims", "claims_issued_in_future", "auth.Claims.Validate", nil)
	}
	if !claims.IssuedAt.IsZero() && !claims.ExpiresAt.After(claims.IssuedAt) {
		return newFault(ErrInvalidClaims, faults.CodeUnauthenticated, "invalid authentication claims", "invalid_claim_lifetime", "auth.Claims.Validate", nil)
	}
	if !claims.NotBefore.IsZero() && !claims.ExpiresAt.After(claims.NotBefore) {
		return newFault(ErrInvalidClaims, faults.CodeUnauthenticated, "invalid authentication claims", "invalid_claim_lifetime", "auth.Claims.Validate", nil)
	}
	if len(claims.TokenID) > 512 || containsControl(claims.TokenID) {
		return newFault(ErrInvalidClaims, faults.CodeUnauthenticated, "invalid authentication claims", "invalid_token_id", "auth.Claims.Validate", nil)
	}
	if len(claims.Audience) > MaximumAudiences {
		return newFault(ErrInvalidClaims, faults.CodeUnauthenticated, "invalid authentication claims", "too_many_audiences", "auth.Claims.Validate", faults.Fields{"audience_count": len(claims.Audience)})
	}
	seenAudiences := make(map[string]struct{}, len(claims.Audience))
	for _, audience := range claims.Audience {
		normalized := strings.TrimSpace(audience)
		if !validIdentityText(normalized, 512) {
			return newFault(ErrInvalidClaims, faults.CodeUnauthenticated, "invalid authentication claims", "invalid_audience", "auth.Claims.Validate", nil)
		}
		if _, exists := seenAudiences[normalized]; exists {
			return newFault(ErrInvalidClaims, faults.CodeUnauthenticated, "invalid authentication claims", "duplicate_audience", "auth.Claims.Validate", nil)
		}
		seenAudiences[normalized] = struct{}{}
	}
	for _, identifier := range []identifiers.ID{claims.PrincipalID, claims.OrganizationID, claims.TenantID} {
		if !identifier.IsZero() {
			if err := identifier.Validate(); err != nil {
				return newFault(errors.Join(ErrInvalidClaims, err), faults.CodeUnauthenticated, "invalid authentication claims", "invalid_claim_identifier", "auth.Claims.Validate", nil)
			}
		}
	}
	if _, err := NewPermissionSet(claims.Permissions...); err != nil {
		return newFault(errors.Join(ErrInvalidClaims, err), faults.CodeUnauthenticated, "invalid authentication claims", "invalid_claim_permission", "auth.Claims.Validate", nil)
	}
	if _, err := normalizeClaimsAttributes(claims.Attributes); err != nil {
		return err
	}
	return nil
}

func PrincipalFromClaims(kind PrincipalKind, claims Claims, now time.Time, skew time.Duration) (Principal, error) {
	if err := claims.Validate(now, skew); err != nil {
		return Principal{}, err
	}
	permissions, err := NewPermissionSet(claims.Permissions...)
	if err != nil {
		return Principal{}, err
	}
	return NewPrincipal(
		kind,
		claims.Subject,
		WithPrincipalID(claims.PrincipalID),
		WithIssuer(claims.Issuer),
		WithOrganizationID(claims.OrganizationID),
		WithTenantID(claims.TenantID),
		WithPermissions(permissions),
		WithPrincipalAttributes(claims.Attributes),
		WithAuthenticationTimes(claims.IssuedAt, claims.ExpiresAt),
	)
}
