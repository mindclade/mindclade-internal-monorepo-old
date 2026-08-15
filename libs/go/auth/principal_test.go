// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package auth

import (
	"testing"
	"time"

	"mindclade.internal/libs/go/identifiers"
)

var (
	testPrincipalID = identifiers.MustParseID("user_018f3f4a5b6c7d8e8f900123456789ab")
	testOrgID       = identifiers.MustParseID("org_018f3f4a5b6c7d8e8f900123456789ac")
	testTenantID    = identifiers.MustParseID("tenant_018f3f4a5b6c7d8e8f900123456789ad")
	testRunID       = identifiers.MustParseID("run_018f3f4a5b6c7d8e8f900123456789ae")
)

func testPrincipal(t *testing.T) Principal {
	t.Helper()
	permissions, err := NewPermissionSet(MustParsePermission("runs.*"))
	if err != nil {
		t.Fatal(err)
	}
	principal, err := NewPrincipal(
		PrincipalKindUser,
		"subject-123",
		WithPrincipalID(testPrincipalID),
		WithIssuer("https://identity.example"),
		WithOrganizationID(testOrgID),
		WithTenantID(testTenantID),
		WithPermissions(permissions),
		WithPrincipalAttributes(map[string]string{"email": "scientist@example.com"}),
		WithAuthenticationTimes(time.Unix(100, 0), time.Unix(200, 0)),
	)
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

func TestPrincipalIsImmutableAndExpires(t *testing.T) {
	t.Parallel()

	principal := testPrincipal(t)
	attributes := principal.Attributes()
	attributes["email"] = "changed@example.com"
	if principal.Attributes()["email"] != "scientist@example.com" {
		t.Fatal("principal attributes mutated through accessor")
	}
	if !principal.Allows(MustParsePermission("runs.read")) {
		t.Fatal("principal did not allow granted wildcard")
	}
	if principal.Expired(time.Unix(199, 0), 0) {
		t.Fatal("principal expired early")
	}
	if !principal.Expired(time.Unix(200, 0), 0) {
		t.Fatal("principal did not expire")
	}
}

func TestPrincipalRequiresIssuerAndSubject(t *testing.T) {
	t.Parallel()

	if _, err := NewPrincipal(PrincipalKindUser, "subject"); err == nil {
		t.Fatal("principal without issuer accepted")
	}
}
