// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package auth

import (
	"context"
	"errors"
	"testing"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
)

func TestPermissionAuthorizer(t *testing.T) {
	t.Parallel()

	resource, err := NewResource(
		ResourceType("run"),
		WithResourceID(testRunID),
		WithResourceOrganizationID(testOrgID),
		WithResourceTenantID(testTenantID),
	)
	if err != nil {
		t.Fatal(err)
	}
	request := AuthorizationRequest{
		Principal:  testPrincipal(t),
		Permission: MustParsePermission("runs.read"),
		Resource:   resource,
	}
	if err := Enforce(context.Background(), PermissionAuthorizer{}, request); err != nil {
		t.Fatal(err)
	}

	request.Permission = MustParsePermission("models.delete")
	err = Enforce(context.Background(), PermissionAuthorizer{}, request)
	if !errors.Is(err, ErrAuthorizationDenied) || !faults.IsCode(err, faults.CodePermissionDenied) || !faults.IsReason(err, "permission_not_granted") {
		t.Fatalf("denial = %v", err)
	}
}

func identifiersForTest(value string) identifiers.ID { return identifiers.MustParseID(value) }

func TestPermissionAuthorizerEnforcesScope(t *testing.T) {
	t.Parallel()

	otherTenant := identifiersForTest("tenant_018f3f4a5b6c7d8e8f900123456789af")
	resource, err := NewResource(ResourceType("run"), WithResourceTenantID(otherTenant))
	if err != nil {
		t.Fatal(err)
	}
	err = Enforce(context.Background(), PermissionAuthorizer{}, AuthorizationRequest{
		Principal: testPrincipal(t), Permission: MustParsePermission("runs.read"), Resource: resource,
	})
	if !faults.IsReason(err, "tenant_scope_mismatch") {
		t.Fatalf("error = %v", err)
	}
}
