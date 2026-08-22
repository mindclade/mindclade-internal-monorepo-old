// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package iapauth

import (
	"context"
	"testing"
	"time"

	"google.golang.org/api/idtoken"

	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
)

type fakeValidator struct {
	payload *idtoken.Payload
	err     error
}

func (validator fakeValidator) Validate(_ context.Context, _ string, audience string) (*idtoken.Payload, error) {
	if validator.payload != nil && validator.payload.Audience != audience {
		return nil, faults.New(faults.CodeUnauthenticated, "audience mismatch")
	}
	return validator.payload, validator.err
}

func TestAuthenticatorSeparatesAdministratorAndEvaluatorPermissions(t *testing.T) {
	now := time.Date(2026, time.August, 21, 20, 0, 0, 0, time.UTC)
	administratorPermissions, err := administratorPolicyPermissions()
	if err != nil {
		t.Fatal(err)
	}
	evaluatorPermissions, err := evaluatorPolicyPermissions()
	if err != nil {
		t.Fatal(err)
	}
	value := &authenticator{
		audience: "/projects/123/global/backendServices/456", evaluatorEmail: "sa-prod-qual-evaluator@security-123.iam.gserviceaccount.com", clock: clock.NewFake(now),
		administratorPermissions: administratorPermissions, evaluatorPermissions: evaluatorPermissions,
		validator: fakeValidator{payload: &idtoken.Payload{
			Issuer: Issuer, Audience: "/projects/123/global/backendServices/456", Subject: "accounts.google.com:123456789",
			IssuedAt: now.Add(-time.Minute).Unix(), Expires: now.Add(9 * time.Minute).Unix(), Claims: map[string]interface{}{"email": "admin@mindclade.com"},
		}},
	}
	credential, err := auth.Bearer("signed-iap-assertion")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := value.Authenticate(context.Background(), credential)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Kind() != auth.PrincipalKindUser || principal.Subject() != "accounts.google.com:123456789" || principal.Issuer() != Issuer {
		t.Fatalf("principal=%+v", principal)
	}
	for _, permission := range []string{
		"ai_gateway.policies.read",
		"ai_gateway.policy_proposals.create",
		"ai_gateway.policy_proposals.approve",
		"evidence.claims.read",
		"production_eligibility.decisions.read",
		"production_eligibility.decisions.revoke",
	} {
		if !principal.Allows(auth.MustParsePermission(permission)) {
			t.Fatalf("principal lacks %s", permission)
		}
	}
	for _, permission := range []string{"evidence.claims.submit", "production_eligibility.decisions.evaluate"} {
		if principal.Allows(auth.MustParsePermission(permission)) {
			t.Fatalf("administrator unexpectedly has %s", permission)
		}
	}

	evaluatorPayload := &idtoken.Payload{
		Issuer: Issuer, Audience: value.audience, Subject: "accounts.google.com:987654321",
		IssuedAt: now.Add(-time.Minute).Unix(), Expires: now.Add(9 * time.Minute).Unix(), Claims: map[string]interface{}{"email": value.evaluatorEmail},
	}
	value.validator = fakeValidator{payload: evaluatorPayload}
	evaluator, err := value.Authenticate(context.Background(), credential)
	if err != nil {
		t.Fatal(err)
	}
	for _, permission := range []string{"evidence.claims.read", "evidence.claims.submit", "production_eligibility.decisions.evaluate", "production_eligibility.decisions.read"} {
		if !evaluator.Allows(auth.MustParsePermission(permission)) {
			t.Fatalf("evaluator lacks %s", permission)
		}
	}
	for _, permission := range []string{"production_eligibility.decisions.revoke", "ai_gateway.policy_proposals.approve"} {
		if evaluator.Allows(auth.MustParsePermission(permission)) {
			t.Fatalf("evaluator unexpectedly has %s", permission)
		}
	}
}

func TestAuthenticatorRejectsIssuerLifetimeSubjectAndValidationFailures(t *testing.T) {
	now := time.Date(2026, time.August, 21, 20, 0, 0, 0, time.UTC)
	administratorPermissions, err := administratorPolicyPermissions()
	if err != nil {
		t.Fatal(err)
	}
	evaluatorPermissions, err := evaluatorPolicyPermissions()
	if err != nil {
		t.Fatal(err)
	}
	base := idtoken.Payload{
		Issuer: Issuer, Audience: "audience", Subject: "accounts.google.com:123",
		IssuedAt: now.Add(-time.Minute).Unix(), Expires: now.Add(9 * time.Minute).Unix(), Claims: map[string]interface{}{"email": "admin@mindclade.com"},
	}
	for name, mutate := range map[string]func(*idtoken.Payload){
		"issuer":  func(payload *idtoken.Payload) { payload.Issuer = "https://accounts.google.com" },
		"future":  func(payload *idtoken.Payload) { payload.IssuedAt = now.Add(time.Minute).Unix() },
		"expired": func(payload *idtoken.Payload) { payload.Expires = now.Unix() },
		"long":    func(payload *idtoken.Payload) { payload.Expires = now.Add(20 * time.Minute).Unix() },
		"subject": func(payload *idtoken.Payload) { payload.Subject = "email@example.com" },
		"email":   func(payload *idtoken.Payload) { payload.Claims = nil },
		"service-account": func(payload *idtoken.Payload) {
			payload.Claims = map[string]interface{}{"email": "other@security-123.iam.gserviceaccount.com"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			payload := base
			mutate(&payload)
			value := &authenticator{audience: "audience", evaluatorEmail: "sa-prod-qual-evaluator@security-123.iam.gserviceaccount.com", clock: clock.NewFake(now), administratorPermissions: administratorPermissions, evaluatorPermissions: evaluatorPermissions, validator: fakeValidator{payload: &payload}}
			credential, err := auth.Bearer("signed")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := value.Authenticate(context.Background(), credential); !faults.IsCode(err, faults.CodeUnauthenticated) {
				t.Fatalf("expected rejection, got %v", err)
			}
		})
	}
}
