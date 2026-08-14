// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package auth

import (
	"errors"
	"testing"
	"time"
)

func TestClaimsValidationAndPrincipalConversion(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()
	claims := Claims{
		Issuer:         "https://identity.example",
		Subject:        "subject-123",
		Audience:       []string{"mindclade-api"},
		IssuedAt:       now.Add(-time.Minute),
		NotBefore:      now.Add(-time.Minute),
		ExpiresAt:      now.Add(time.Hour),
		PrincipalID:    testPrincipalID,
		OrganizationID: testOrgID,
		TenantID:       testTenantID,
		Permissions:    []Permission{MustParsePermission("runs.read")},
		Attributes:     map[string]string{"email": "scientist@example.com"},
	}
	principal, err := PrincipalFromClaims(PrincipalKindUser, claims, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Subject() != claims.Subject || !principal.Allows(MustParsePermission("runs.read")) {
		t.Fatalf("principal = %#v", principal)
	}

	claims.ExpiresAt = now
	if err := claims.Validate(now, 0); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired claims error = %v", err)
	}
}
