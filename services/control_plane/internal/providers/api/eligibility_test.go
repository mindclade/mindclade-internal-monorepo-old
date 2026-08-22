// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mindclade.dev/control/evidence"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/services/control_plane/internal/config"
)

type eligibilityHTTPFixture struct {
	revoked identifiers.Digest
}

func (fixture *eligibilityHTTPFixture) Record(context.Context, evidence.Claim, evidence.Verification) error {
	return nil
}

func (fixture *eligibilityHTTPFixture) Evidence(context.Context, identifiers.Digest) ([]evidence.Record, error) {
	return []evidence.Record{}, nil
}

func (fixture *eligibilityHTTPFixture) Evaluate(context.Context, evidence.DeploymentBundle) (evidence.SignedDecision, error) {
	return evidence.SignedDecision{}, nil
}

func (fixture *eligibilityHTTPFixture) GetDecision(context.Context, identifiers.Digest) (evidence.SignedDecision, bool, error) {
	return evidence.SignedDecision{}, false, nil
}

func (fixture *eligibilityHTTPFixture) Revoke(_ context.Context, digest identifiers.Digest, _ string) error {
	fixture.revoked = digest
	return nil
}

func TestEligibilityAuthorizationMapsEveryRoute(t *testing.T) {
	digest := identifiers.SHA256String("authorization").String()
	for _, testCase := range []struct {
		method     string
		path       string
		permission string
	}{
		{http.MethodPost, "/v1/evidence/claims", "evidence.claims.submit"},
		{http.MethodGet, "/v1/evidence/subjects/" + digest + "/claims", "evidence.claims.read"},
		{http.MethodPost, "/v1/production-eligibility/decisions", "production_eligibility.decisions.evaluate"},
		{http.MethodGet, "/v1/production-eligibility/decisions/" + digest, "production_eligibility.decisions.read"},
		{http.MethodPost, "/v1/production-eligibility/decisions/" + digest + "/revocations", "production_eligibility.decisions.revoke"},
	} {
		request := httptest.NewRequest(testCase.method, testCase.path, nil)
		target, mapped, err := resolveEligibilityAuthorization(request)
		if err != nil {
			t.Fatal(err)
		}
		if !mapped || target.Permission.String() != testCase.permission {
			t.Fatalf("%s %s mapped=%v permission=%q", testCase.method, testCase.path, mapped, target.Permission.String())
		}
	}
}

func TestEligibilityRevocationRoute(t *testing.T) {
	fixture := &eligibilityHTTPFixture{}
	handler, err := newEligibilityMux(fixture)
	if err != nil {
		t.Fatal(err)
	}
	digest := identifiers.SHA256String("decision")
	request := httptest.NewRequest(http.MethodPost, "/v1/production-eligibility/decisions/"+digest.String()+"/revocations", strings.NewReader(`{"reason":"artifact_compromised"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !fixture.revoked.Equal(digest) {
		t.Fatalf("status=%d revoked=%s body=%s", response.Code, fixture.revoked.String(), response.Body.String())
	}
}

func TestProductionAdminRequiresEligibilityConfiguration(t *testing.T) {
	engine, err := newEligibilityEngine(context.Background(), config.Settings{Environment: config.EnvironmentProduction}, nil, nil)
	if engine != nil || !faults.IsReason(err, "eligibility_configuration_required") {
		t.Fatalf("engine=%v err=%v", engine, err)
	}
}
