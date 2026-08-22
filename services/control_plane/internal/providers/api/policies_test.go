// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/signing"
	"go.mindclade.dev/services/control_plane/internal/transport"
)

type policyHTTPFixture struct {
	handler     http.Handler
	service     admission.GovernanceService
	proposer    auth.Principal
	approver    auth.Principal
	tenantID    identifiers.ID
	workspaceID identifiers.ID
	now         time.Time
}

func newPolicyHTTPFixture(t *testing.T) policyHTTPFixture {
	t.Helper()
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	value := clock.NewFake(now)
	ids, err := identifiers.NewGenerator(identifiers.WithTimeSource(value.Now))
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(index + 1)
	}
	signer, err := signing.NewHMACSigner(signing.MustParseKeyID("policy-http-test"), key)
	if err != nil {
		t.Fatal(err)
	}
	proposer, err := auth.NewPrincipal(auth.PrincipalKindUser, "admin-a", auth.WithIssuer("https://cloud.google.com/iap"))
	if err != nil {
		t.Fatal(err)
	}
	approver, err := auth.NewPrincipal(auth.PrincipalKindUser, "admin-b", auth.WithIssuer("https://cloud.google.com/iap"))
	if err != nil {
		t.Fatal(err)
	}
	tenantID, err := ids.ID(identifiers.MustParseKind("tenant"))
	if err != nil {
		t.Fatal(err)
	}
	workspaceID, err := ids.ID(identifiers.MustParseKind("workspace"))
	if err != nil {
		t.Fatal(err)
	}
	service := admission.GovernanceService{Repository: admission.NewMemoryRepository(100), IDs: ids, Clock: value, Signer: signer}
	handler, err := newPolicyMux(service)
	if err != nil {
		t.Fatal(err)
	}
	return policyHTTPFixture{
		handler: transport.Preconditions()(handler), service: service, proposer: proposer, approver: approver,
		tenantID: tenantID, workspaceID: workspaceID, now: now,
	}
}

func (fixture policyHTTPFixture) spec() admission.WorkspacePolicyBundleSpec {
	maximum := admission.Quota{
		admission.UnitRequests: 1, admission.UnitInputTokens: 2_000,
		admission.UnitOutputTokens: 1_000, admission.UnitCostMicros: 25_000,
	}
	return admission.WorkspacePolicyBundleSpec{
		TenantID: fixture.tenantID, WorkspaceID: fixture.workspaceID, MLflowWorkspace: "research-team", PolicyEpoch: 1,
		EffectiveAt: fixture.now.Add(-time.Minute), ExpiresAt: fixture.now.Add(2 * time.Hour),
		Endpoints: []admission.EndpointPolicy{{
			Name: "chat-primary", Route: admission.GatewayRoute{Endpoint: "chat-primary", Provider: "openai-compatible", Model: "qualified-model"},
			Operations: []admission.GatewayOperation{admission.OperationChatCompletions}, ConnectionRef: "provider-primary",
			MaximumBodyBytes: 1 << 20, MaximumRequest: maximum, PricingVersion: 1,
			InputMicrosPerMillion: 1_000_000, OutputMicrosPerMillion: 2_000_000,
			MetadataOnlyTracing: true, UsageTracking: false,
		}},
		Subjects: []admission.SubjectPolicy{{Subject: "gateway-client", Endpoints: []string{"chat-primary"}, MaximumRequest: maximum}},
		Budget: admission.BundleBudgetSpec{
			Limit:    admission.Quota{admission.UnitRequests: 10, admission.UnitInputTokens: 20_000, admission.UnitOutputTokens: 10_000, admission.UnitCostMicros: 250_000},
			StartsAt: fixture.now.Add(-time.Minute), ExpiresAt: fixture.now.Add(2 * time.Hour),
		},
	}
}

func (fixture policyHTTPFixture) call(t *testing.T, principal auth.Principal, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	ctx, err := auth.WithPrincipal(request.Context(), principal)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request.WithContext(ctx))
	return response
}

func TestPolicyHTTPRequiresConditionalTwoPersonPublication(t *testing.T) {
	fixture := newPolicyHTTPFixture(t)
	base := policyWorkspacePrefix + fixture.workspaceID.String()
	missing := fixture.call(t, fixture.proposer, http.MethodPost, base+"/policy-proposals", fixture.spec(), nil)
	if missing.Code != http.StatusPreconditionFailed || policyProblem(t, missing).Reason != "policy_precondition_required" {
		t.Fatalf("missing precondition status=%d body=%s", missing.Code, missing.Body.String())
	}
	created := fixture.call(t, fixture.proposer, http.MethodPost, base+"/policy-proposals", fixture.spec(), map[string]string{"If-None-Match": "*"})
	if created.Code != http.StatusCreated || created.Header().Get("ETag") == "" || created.Header().Get("Location") == "" {
		t.Fatalf("proposal status=%d headers=%v body=%s", created.Code, created.Header(), created.Body.String())
	}
	var proposal admission.PolicyProposal
	if err := json.Unmarshal(created.Body.Bytes(), &proposal); err != nil {
		t.Fatal(err)
	}
	path := base + "/policy-proposals/" + proposal.ID.String() + "/approval"
	self := fixture.call(t, fixture.proposer, http.MethodPost, path, nil, map[string]string{"If-Match": proposal.Version.ETag()})
	if self.Code != http.StatusForbidden || policyProblem(t, self).Reason != "policy_self_approval_forbidden" {
		t.Fatalf("self approval status=%d body=%s", self.Code, self.Body.String())
	}
	approved := fixture.call(t, fixture.approver, http.MethodPost, path, nil, map[string]string{"If-Match": proposal.Version.ETag()})
	if approved.Code != http.StatusOK || approved.Header().Get("ETag") == "" {
		t.Fatalf("approval status=%d headers=%v body=%s", approved.Code, approved.Header(), approved.Body.String())
	}
	var publication approvalHTTPResponse
	if err := json.Unmarshal(approved.Body.Bytes(), &publication); err != nil {
		t.Fatal(err)
	}
	if publication.Bundle.Spec.PolicyEpoch != 1 || publication.Receipt.ProposerKey == publication.Receipt.ApproverKey {
		t.Fatalf("publication=%+v", publication)
	}
	current := fixture.call(t, fixture.approver, http.MethodGet, base+"/policy", nil, nil)
	if current.Code != http.StatusOK || current.Header().Get("ETag") != publication.Bundle.Version.ETag() {
		t.Fatalf("current policy status=%d headers=%v body=%s", current.Code, current.Header(), current.Body.String())
	}
}

func TestPolicyHTTPBindsWorkspaceAndRejectsAmbiguousConditions(t *testing.T) {
	fixture := newPolicyHTTPFixture(t)
	other, err := identifiers.NewIDAt(identifiers.MustParseKind("workspace"), fixture.now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	path := policyWorkspacePrefix + other.String() + "/policy-proposals"
	mismatch := fixture.call(t, fixture.proposer, http.MethodPost, path, fixture.spec(), map[string]string{"If-None-Match": "*"})
	if mismatch.Code != http.StatusBadRequest || policyProblem(t, mismatch).Reason != "policy_workspace_mismatch" {
		t.Fatalf("workspace mismatch status=%d body=%s", mismatch.Code, mismatch.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, policyWorkspacePrefix+fixture.workspaceID.String()+"/policy-proposals", bytes.NewReader([]byte(`{}`)))
	request.Header.Add("If-None-Match", "*")
	request.Header.Add("If-None-Match", "*")
	ctx, err := auth.WithPrincipal(request.Context(), fixture.proposer)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request.WithContext(ctx))
	if response.Code != http.StatusBadRequest || policyProblem(t, response).Reason != "conditional_header_ambiguous" {
		t.Fatalf("ambiguous condition status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPolicyAndAdmissionRoutesCannotCrossMount(t *testing.T) {
	fixture := newPolicyHTTPFixture(t)
	policyResponse := fixture.call(t, fixture.proposer, http.MethodPost, reservationsPath, map[string]any{}, nil)
	if policyResponse.Code != http.StatusMethodNotAllowed && policyResponse.Code != http.StatusNotFound {
		t.Fatalf("policy mux mounted admission route: %d", policyResponse.Code)
	}
	admissionFixture := newAdmissionHTTPFixture(t)
	request := httptest.NewRequest(http.MethodGet, policyWorkspacePrefix+fixture.workspaceID.String()+"/policy", nil)
	response := httptest.NewRecorder()
	admissionFixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed && response.Code != http.StatusNotFound {
		t.Fatalf("admission mux mounted policy route: %d", response.Code)
	}
}

func TestPolicyAuthorizationMapsEveryAdminAction(t *testing.T) {
	fixture := newPolicyHTTPFixture(t)
	proposalID, err := identifiers.NewIDAt(identifiers.MustParseKind("policyproposal"), fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	base := policyWorkspacePrefix + fixture.workspaceID.String()
	for _, test := range []struct {
		method, path, permission string
	}{
		{http.MethodGet, base + "/policy", "ai_gateway.policies.read"},
		{http.MethodPost, base + "/policy-proposals", "ai_gateway.policy_proposals.create"},
		{http.MethodGet, base + "/policy-proposals/" + proposalID.String(), "ai_gateway.policy_proposals.read"},
		{http.MethodPost, base + "/policy-proposals/" + proposalID.String() + "/approval", "ai_gateway.policy_proposals.approve"},
		{http.MethodPost, base + "/policy-proposals/" + proposalID.String() + "/rejection", "ai_gateway.policy_proposals.reject"},
		{http.MethodPost, base + "/policy-proposals/" + proposalID.String() + "/cancellation", "ai_gateway.policy_proposals.cancel"},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		target, mapped, err := resolvePolicyAuthorization(request)
		if err != nil || !mapped || target.Permission.String() != test.permission {
			t.Fatalf("%s %s mapped=%v target=%+v err=%v", test.method, test.path, mapped, target, err)
		}
	}
}

func policyProblem(t *testing.T, response *httptest.ResponseRecorder) httpx.Problem {
	t.Helper()
	var problem httpx.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v body=%s", err, response.Body.String())
	}
	return problem
}

var _ policyEngine = admission.GovernanceService{}
