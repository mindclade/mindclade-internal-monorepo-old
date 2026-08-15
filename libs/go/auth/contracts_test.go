// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"mindclade.internal/libs/go/faults"
)

func TestCredentialRedactionAndAttributes(t *testing.T) {
	t.Parallel()

	attributes := map[string]string{"provider": "internal"}
	credential, err := NewCredential(
		CredentialSchemeAPIKey,
		[]byte("super-secret-api-key"),
		WithCredentialAttributes(attributes),
	)
	if err != nil {
		t.Fatal(err)
	}
	attributes["provider"] = "mutated"
	if credential.Scheme() != CredentialSchemeAPIKey || credential.IsZero() {
		t.Fatalf("credential = %#v", credential)
	}
	if credential.Attributes()["provider"] != "internal" {
		t.Fatal("credential attributes were not copied")
	}
	for _, formatted := range []string{
		fmt.Sprintf("%s", credential),
		fmt.Sprintf("%v", credential),
		fmt.Sprintf("%#v", credential),
		fmt.Sprintf("%x", credential),
	} {
		if strings.Contains(formatted, "super-secret") || !strings.Contains(formatted, "REDACTED") {
			t.Fatalf("credential formatting leaked or omitted redaction: %q", formatted)
		}
	}
	copyOfValue := credential.Value()
	copyOfValue[0] = 'X'
	if string(credential.Value()) != "super-secret-api-key" {
		t.Fatal("credential value was mutated through accessor")
	}

	apiKey, err := APIKey("another-secret")
	if err != nil || apiKey.Scheme() != CredentialSchemeAPIKey {
		t.Fatalf("APIKey() = %#v, %v", apiKey, err)
	}
	if _, err := NewCredential(CredentialSchemeBearer, []byte("token"), WithCredentialAttributes(map[string]string{"api-key": "value"})); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("sensitive attribute error = %v", err)
	}
}

func TestClaimsValidationEdgesAndClone(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	claims := Claims{
		Issuer:      "https://identity.example",
		Subject:     "subject-123",
		Audience:    []string{"mindclade-api"},
		IssuedAt:    now.Add(-time.Minute),
		NotBefore:   now.Add(-time.Minute),
		ExpiresAt:   now.Add(time.Hour),
		TokenID:     "token-id-123",
		Permissions: []Permission{MustParsePermission("runs.read")},
		Attributes:  map[string]string{"email": "scientist@example.com"},
	}
	clone := claims.Clone()
	claims.Audience[0] = "mutated"
	claims.Permissions[0] = MustParsePermission("models.read")
	claims.Attributes["email"] = "mutated@example.com"
	if clone.Audience[0] != "mindclade-api" || clone.Permissions[0] != MustParsePermission("runs.read") || clone.Attributes["email"] != "scientist@example.com" {
		t.Fatalf("Clone() = %#v", clone)
	}

	invalidLifetime := clone
	invalidLifetime.NotBefore = invalidLifetime.ExpiresAt
	if err := invalidLifetime.Validate(now, 0); !errors.Is(err, ErrInvalidClaims) || !faults.IsCode(err, faults.CodeUnauthenticated) {
		t.Fatalf("invalid lifetime error = %v", err)
	}
	invalidTokenID := clone
	invalidTokenID.TokenID = "bad\ntoken"
	if err := invalidTokenID.Validate(now, 0); !errors.Is(err, ErrInvalidClaims) {
		t.Fatalf("invalid token ID error = %v", err)
	}
	invalidPermission := clone
	invalidPermission.Permissions = []Permission{"Bad Permission"}
	if err := invalidPermission.Validate(now, 0); !errors.Is(err, ErrInvalidClaims) || !faults.IsCode(err, faults.CodeUnauthenticated) {
		t.Fatalf("invalid permission error = %v", err)
	}
}

func TestPermissionSetAndDecisionContracts(t *testing.T) {
	t.Parallel()

	for _, invalid := range []string{"Runs.read", ".runs", "runs..read", "runs.-admin", "runs.admin-", "runs.*.read"} {
		if _, err := ParsePermission(invalid); !errors.Is(err, ErrInvalidPermission) {
			t.Fatalf("ParsePermission(%q) error = %v", invalid, err)
		}
	}
	left, err := NewPermissionSet(MustParsePermission("runs.read"), MustParsePermission("models.*"))
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewPermissionSet(MustParsePermission("runs.write"))
	if err != nil {
		t.Fatal(err)
	}
	merged := left.Merge(right)
	if merged.Len() != 3 || merged.IsZero() || !merged.Contains(MustParsePermission("runs.write")) || !merged.Allows(MustParsePermission("models.release.promote")) {
		t.Fatalf("merged permissions = %#v", merged.Values())
	}
	values := merged.Values()
	if len(values) != 3 || values[0].String() > values[1].String() {
		t.Fatalf("Values() not sorted: %#v", values)
	}

	obligations := map[string]string{"audit_level": "high"}
	decision, err := NewDecision(EffectAllow, "policy_allow", "policy-1", obligations)
	if err != nil {
		t.Fatal(err)
	}
	obligations["audit_level"] = "mutated"
	if decision.Effect() != EffectAllow || decision.PolicyID() != "policy-1" || decision.Obligations()["audit_level"] != "high" {
		t.Fatalf("decision = %#v", decision)
	}
	if err := Abstain("no_matching_policy").Validate(); err != nil {
		t.Fatal(err)
	}
	if err := Deny("").Validate(); !errors.Is(err, ErrInvalidDecision) {
		t.Fatalf("empty deny reason error = %v", err)
	}
}

func TestScopedAuthorizationFailsClosed(t *testing.T) {
	t.Parallel()

	permissions, err := NewPermissionSet(MustParsePermission("runs.read"))
	if err != nil {
		t.Fatal(err)
	}
	unscopedUser, err := NewPrincipal(
		PrincipalKindUser,
		"user-123",
		WithIssuer("https://identity.example"),
		WithPermissions(permissions),
	)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := NewResource(
		ResourceType("run"),
		WithResourceID(testRunID),
		WithResourceOrganizationID(testOrgID),
	)
	if err != nil {
		t.Fatal(err)
	}
	request := AuthorizationRequest{Principal: unscopedUser, Permission: MustParsePermission("runs.read"), Resource: resource}
	err = Enforce(context.Background(), PermissionAuthorizer{}, request)
	if !errors.Is(err, ErrAuthorizationDenied) || errors.Is(err, ErrUnauthenticated) || !faults.IsReason(err, "organization_scope_mismatch") {
		t.Fatalf("unscoped user denial = %v", err)
	}

	system, err := NewPrincipal(
		PrincipalKindSystem,
		"control-plane",
		WithIssuer("mindclade"),
		WithPermissions(permissions),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Principal = system
	if err := Enforce(context.Background(), PermissionAuthorizer{}, request); err != nil {
		t.Fatalf("system principal should be globally scoped: %v", err)
	}
}

func TestTypedNilProvidersFailClosed(t *testing.T) {
	t.Parallel()

	credential, _ := Bearer("token")
	var authenticator AuthenticatorFunc
	if _, err := Authenticate(context.Background(), authenticator, credential); !errors.Is(err, ErrNilAuthenticator) {
		t.Fatalf("typed nil authenticator error = %v", err)
	}

	resource, _ := NewResource(ResourceType("run"))
	request := AuthorizationRequest{Principal: testPrincipal(t), Permission: MustParsePermission("runs.read"), Resource: resource}
	var authorizer AuthorizerFunc
	if err := Enforce(context.Background(), authorizer, request); !errors.Is(err, ErrNilAuthorizer) {
		t.Fatalf("typed nil authorizer error = %v", err)
	}
}

func TestPrincipalAndResourceAccessors(t *testing.T) {
	t.Parallel()

	kind, err := ParsePrincipalKind("SYSTEM")
	if err != nil || kind != PrincipalKindSystem {
		t.Fatalf("ParsePrincipalKind() = %q, %v", kind, err)
	}
	if _, err := ParsePrincipalKind("unknown"); !errors.Is(err, ErrInvalidPrincipal) {
		t.Fatalf("invalid kind error = %v", err)
	}

	principal := testPrincipal(t)
	if principal.IsZero() || principal.ID() != testPrincipalID || principal.Issuer() == "" || principal.Permissions().Len() == 0 || principal.AuthenticatedAt().IsZero() || principal.ExpiresAt().IsZero() {
		t.Fatalf("principal accessors = %#v", principal)
	}

	resourceType, err := ParseResourceType("model.release")
	if err != nil || resourceType.String() != "model.release" {
		t.Fatalf("ParseResourceType() = %q, %v", resourceType, err)
	}
	if _, err := ParseResourceType(".model"); !errors.Is(err, ErrInvalidResource) {
		t.Fatalf("invalid resource type error = %v", err)
	}
	attributes := map[string]string{"region": "us-central1"}
	resource, err := NewResource(resourceType, WithResourceAttributes(attributes))
	if err != nil {
		t.Fatal(err)
	}
	attributes["region"] = "mutated"
	if resource.Attributes()["region"] != "us-central1" {
		t.Fatal("resource attributes were not copied")
	}
}

func TestPrincipalKeyIsPrivateAndCollisionResistantForDelimitedSubjects(t *testing.T) {
	t.Parallel()

	left, err := NewPrincipal(PrincipalKindUser, "b|c", WithIssuer("a"))
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewPrincipal(PrincipalKindUser, "c", WithIssuer("a|b"))
	if err != nil {
		t.Fatal(err)
	}
	if left.Key() == right.Key() || left.Key() == left.Issuer()+"|"+left.Subject() || !strings.HasPrefix(left.Key(), "sha256:") {
		t.Fatalf("principal keys left=%q right=%q", left.Key(), right.Key())
	}
	if (Principal{}).Key() != "" {
		t.Fatal("zero principal returned a key")
	}
}

func TestClaimsAndPermissionSetBounds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	claims := Claims{
		Issuer:    "mindclade",
		Subject:   "user-42",
		ExpiresAt: now.Add(time.Hour),
		Audience:  []string{"api", "api"},
	}
	if err := claims.Validate(now, 0); !errors.Is(err, ErrInvalidClaims) || !faults.IsReason(err, "duplicate_audience") {
		t.Fatalf("duplicate audience error = %v", err)
	}
	claims.Audience = make([]string, MaximumAudiences+1)
	for index := range claims.Audience {
		claims.Audience[index] = fmt.Sprintf("audience-%d", index)
	}
	if err := claims.Validate(now, 0); !faults.IsReason(err, "too_many_audiences") {
		t.Fatalf("audience bound error = %v", err)
	}

	leftValues := make([]Permission, 0, MaximumPermissions)
	rightValues := make([]Permission, 0, MaximumPermissions)
	for index := 0; index < MaximumPermissions; index++ {
		leftValues = append(leftValues, MustParsePermission(fmt.Sprintf("left.permission%d", index)))
		rightValues = append(rightValues, MustParsePermission(fmt.Sprintf("right.permission%d", index)))
	}
	leftSet, err := NewPermissionSet(leftValues...)
	if err != nil {
		t.Fatal(err)
	}
	rightSet, err := NewPermissionSet(rightValues...)
	if err != nil {
		t.Fatal(err)
	}
	if err := leftSet.Merge(rightSet).Validate(); !errors.Is(err, ErrInvalidPermission) || !faults.IsReason(err, "too_many_permissions") {
		t.Fatalf("merged permission bound error = %v", err)
	}
}

func TestDecisionReasonAndProviderFailureClassification(t *testing.T) {
	t.Parallel()

	if _, err := NewDecision(EffectDeny, "Not Allowed", "policy-1", nil); !errors.Is(err, ErrInvalidDecision) {
		t.Fatalf("non-canonical decision reason error = %v", err)
	}

	credential, err := Bearer("opaque-token")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Authenticate(context.Background(), AuthenticatorFunc(func(context.Context, Credential) (Principal, error) {
		return Principal{}, errors.New("provider exploded")
	}), credential)
	if !faults.IsCode(err, faults.CodeInternal) || faults.PublicMessageOf(err) != "internal error" {
		t.Fatalf("unstructured authentication failure = %v", err)
	}
}
