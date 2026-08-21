// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/httpx/middleware"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
	"go.mindclade.dev/services/control_plane/internal/providers/admissionmetrics"
	"go.mindclade.dev/services/control_plane/internal/transport"
)

var admissionHTTPNow = time.Date(2026, time.August, 21, 9, 0, 0, 0, time.UTC)

type admissionHTTPFixture struct {
	handler    http.Handler
	service    admission.Service
	repository *admission.MemoryRepository
	principal  auth.Principal
	request    admitHTTPRequest
}

func newAdmissionHTTPFixture(t *testing.T) admissionHTTPFixture {
	return newAdmissionHTTPFixtureWithMetrics(t, nil)
}

func newAdmissionHTTPFixtureWithMetrics(t *testing.T, metrics admissionmetrics.Recorder) admissionHTTPFixture {
	t.Helper()
	fake := clock.NewFake(admissionHTTPNow)
	repository := admission.NewMemoryRepository(100)
	route := admission.GatewayRoute{Endpoint: "chat-primary", Provider: "vertex", Model: "gemini-pro"}
	entitlement := admission.Entitlement{
		ID: admissionHTTPID(t, "entitlement", admissionHTTPNow), Subject: "gateway-client", Workspace: "research-team",
		PolicyEpoch: 11, Routes: []admission.GatewayRoute{route},
		MaximumRequest: admission.Quota{admission.UnitRequests: 1, admission.UnitInputTokens: 1000, admission.UnitOutputTokens: 500, admission.UnitCostMicros: 5000},
		NotBefore:      admissionHTTPNow.Add(-time.Minute), ExpiresAt: admissionHTTPNow.Add(time.Hour),
	}
	var err error
	entitlement, err = entitlement.Seal(1)
	if err != nil {
		t.Fatal(err)
	}
	budget := admission.Budget{
		ID: admissionHTTPID(t, "budget", admissionHTTPNow.Add(time.Millisecond)), Workspace: "research-team",
		Limit:    admission.Quota{admission.UnitRequests: 10, admission.UnitInputTokens: 10_000, admission.UnitOutputTokens: 5000, admission.UnitCostMicros: 50_000},
		StartsAt: admissionHTTPNow.Add(-time.Minute), ExpiresAt: admissionHTTPNow.Add(time.Hour),
	}
	budget, err = budget.Seal(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.PutEntitlement(context.Background(), entitlement); err != nil {
		t.Fatal(err)
	}
	if err := repository.PutBudget(context.Background(), budget); err != nil {
		t.Fatal(err)
	}
	service := admission.Service{Repository: repository, Clock: fake, MaximumTTL: time.Minute}
	handler, err := newAdmissionMux(service, metrics)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := auth.NewPrincipal(auth.PrincipalKindService, "gateway-client", auth.WithIssuer("test"))
	if err != nil {
		t.Fatal(err)
	}
	return admissionHTTPFixture{
		handler: transport.Preconditions()(handler), service: service, repository: repository, principal: principal,
		request: admitHTTPRequest{
			RequestDigest: identifiers.SHA256String("provider-payload"), Workspace: "research-team", Route: route,
			PolicyEpoch: 11,
			Requested:   admission.Quota{admission.UnitRequests: 1, admission.UnitInputTokens: 100, admission.UnitOutputTokens: 50, admission.UnitCostMicros: 500},
			TTLSeconds:  30,
		},
	}
}

func admissionHTTPID(t *testing.T, kind string, at time.Time) identifiers.ID {
	t.Helper()
	id, err := identifiers.NewIDAt(identifiers.MustParseKind(kind), at)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (fixture admissionHTTPFixture) call(t *testing.T, method, path string, body any, headers map[string]string, principal auth.Principal) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	requestHeaders := make(http.Header, len(headers)+1)
	requestHeaders.Set("Content-Type", "application/json")
	for name, value := range headers {
		requestHeaders.Set(name, value)
	}
	return callAdmissionHTTP(t, fixture.handler, method, path, payload, requestHeaders, &principal)
}

func callAdmissionHTTP(t *testing.T, handler http.Handler, method, path string, payload []byte, headers http.Header, principal *auth.Principal) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	request.Header = headers.Clone()
	if principal != nil {
		ctx, err := auth.WithPrincipal(request.Context(), *principal)
		if err != nil {
			t.Fatal(err)
		}
		request = request.WithContext(ctx)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func admissionProblem(t *testing.T, response *httptest.ResponseRecorder) httpx.Problem {
	t.Helper()
	var problem httpx.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v; body=%s", err, response.Body.String())
	}
	return problem
}

func (fixture admissionHTTPFixture) guardedHandler(t *testing.T) http.Handler {
	t.Helper()
	admissionPermissions, err := auth.NewPermissionSet(auth.MustParsePermission("ai_gateway.reservations.*"))
	if err != nil {
		t.Fatal(err)
	}
	deniedPermissions, err := auth.NewPermissionSet(auth.MustParsePermission("artifacts.read"))
	if err != nil {
		t.Fatal(err)
	}
	principal := func(subject string, permissions auth.PermissionSet) auth.Principal {
		value, err := auth.NewPrincipal(auth.PrincipalKindService, subject,
			auth.WithIssuer("admission-http-test"), auth.WithPermissions(permissions))
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	principals := map[string]auth.Principal{
		"owner-token":   principal("gateway-client", admissionPermissions),
		"foreign-token": principal("other-client", admissionPermissions),
		"denied-token":  principal("gateway-client", deniedPermissions),
	}
	authenticator := auth.AuthenticatorFunc(func(_ context.Context, credential auth.Credential) (auth.Principal, error) {
		resolved, ok := principals[string(credential.Value())]
		if !ok {
			return auth.Principal{}, faults.New(faults.CodeUnauthenticated, "authentication failed", faults.WithReason("test_credential_unknown"))
		}
		return resolved, nil
	})
	return middleware.Server(fixture.handler, middleware.StackConfig{
		MaximumBodyBytes: maximumRequestBody,
		Authentication:   &middleware.AuthenticationConfig{Authenticator: authenticator},
		Authorization: &middleware.AuthorizationConfig{
			Authorizer: auth.PermissionAuthorizer{}, Resolver: middleware.AuthorizationResolverFunc(resolveAdmissionAuthorization), RequireMapping: true,
		},
	})
}

func TestAdmissionHTTPReserveReplayAndCommit(t *testing.T) {
	fixture := newAdmissionHTTPFixture(t)
	headers := map[string]string{httpx.HeaderIdempotencyKey: "request-0001"}
	created := fixture.call(t, http.MethodPost, reservationsPath, fixture.request, headers, fixture.principal)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	if bytes.Contains(created.Body.Bytes(), []byte("request_digest")) || bytes.Contains(created.Body.Bytes(), []byte("idempotency")) {
		t.Fatalf("ownership proof leaked into response: %s", created.Body.String())
	}
	if bytes.Contains(created.Body.Bytes(), []byte("finalized_at")) {
		t.Fatalf("reserved response included terminal timestamp: %s", created.Body.String())
	}
	var decision admission.Decision
	if err := json.Unmarshal(created.Body.Bytes(), &decision); err != nil {
		t.Fatal(err)
	}
	if decision.Replayed || created.Header().Get("ETag") != decision.Reservation.Version.ETag() {
		t.Fatalf("create replayed=%t etag=%q", decision.Replayed, created.Header().Get("ETag"))
	}

	replay := fixture.call(t, http.MethodPost, reservationsPath, fixture.request, headers, fixture.principal)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	var replayed admission.Decision
	if err := json.Unmarshal(replay.Body.Bytes(), &replayed); err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Reservation.ID.String() != decision.Reservation.ID.String() {
		t.Fatalf("unexpected replay: %+v", replayed)
	}

	commitPath := reservationPrefix + decision.Reservation.ID.String() + "/commit"
	committed := fixture.call(t, http.MethodPost, commitPath, finalizeHTTPRequest{
		RequestDigest: fixture.request.RequestDigest,
		Actual:        admission.Quota{admission.UnitRequests: 1, admission.UnitInputTokens: 90, admission.UnitOutputTokens: 40, admission.UnitCostMicros: 450},
	}, map[string]string{"If-Match": decision.Reservation.Version.ETag()}, fixture.principal)
	if committed.Code != http.StatusOK {
		t.Fatalf("commit status=%d body=%s", committed.Code, committed.Body.String())
	}
	var terminal admission.Decision
	if err := json.Unmarshal(committed.Body.Bytes(), &terminal); err != nil {
		t.Fatal(err)
	}
	if terminal.Reservation.State != admission.ReservationCommitted || terminal.Reservation.Version.Generation() != 2 {
		t.Fatalf("unexpected terminal reservation: %+v", terminal.Reservation)
	}
	if terminal.Reservation.FinalizedAt.IsZero() || !bytes.Contains(committed.Body.Bytes(), []byte("finalized_at")) {
		t.Fatalf("terminal response omitted finalization timestamp: %s", committed.Body.String())
	}
}

func TestAdmissionHTTPFinalizationRequiresVersionAndOwner(t *testing.T) {
	fixture := newAdmissionHTTPFixture(t)
	created := fixture.call(t, http.MethodPost, reservationsPath, fixture.request,
		map[string]string{httpx.HeaderIdempotencyKey: "request-0002"}, fixture.principal)
	var decision admission.Decision
	if err := json.Unmarshal(created.Body.Bytes(), &decision); err != nil {
		t.Fatal(err)
	}
	path := reservationPrefix + decision.Reservation.ID.String() + "/release"
	body := finalizeHTTPRequest{RequestDigest: fixture.request.RequestDigest}
	missingVersion := fixture.call(t, http.MethodPost, path, body, nil, fixture.principal)
	if missingVersion.Code != http.StatusPreconditionFailed {
		t.Fatalf("missing If-Match status=%d body=%s", missingVersion.Code, missingVersion.Body.String())
	}
	foreign, err := auth.NewPrincipal(auth.PrincipalKindService, "other-client", auth.WithIssuer("test"))
	if err != nil {
		t.Fatal(err)
	}
	wrongOwner := fixture.call(t, http.MethodPost, path, body,
		map[string]string{"If-Match": decision.Reservation.Version.ETag()}, foreign)
	if wrongOwner.Code != http.StatusForbidden {
		t.Fatalf("wrong owner status=%d body=%s", wrongOwner.Code, wrongOwner.Body.String())
	}
	stored, err := fixture.repository.Get(context.Background(), decision.Reservation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != admission.ReservationReserved {
		t.Fatalf("foreign caller changed reservation state to %s", stored.State)
	}
}

func TestAdmissionAuthorizationIsExplicitAndUnknownRoutesStayUnmapped(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, reservationsPath, nil)
	target, mapped, err := resolveAdmissionAuthorization(request)
	if err != nil || !mapped || target.Permission.String() != "ai_gateway.reservations.create" {
		t.Fatalf("target=%+v mapped=%t error=%v", target, mapped, err)
	}
	if !target.Resource.ID().IsZero() {
		t.Fatalf("collection authorization unexpectedly targeted ID %s", target.Resource.ID())
	}
	reservationID := admissionHTTPID(t, "reservation", admissionHTTPNow)
	for _, action := range []string{"commit", "release"} {
		request = httptest.NewRequest(http.MethodPost, reservationPrefix+reservationID.String()+"/"+action, nil)
		target, mapped, err = resolveAdmissionAuthorization(request)
		if err != nil || !mapped || target.Resource.ID().String() != reservationID.String() {
			t.Fatalf("%s target=%+v mapped=%t error=%v", action, target, mapped, err)
		}
	}
	invalid := httptest.NewRequest(http.MethodPost, reservationPrefix+"not-a-reservation/commit", nil)
	if _, mapped, err := resolveAdmissionAuthorization(invalid); err == nil || mapped || faults.ReasonOf(err) != "reservation_id_invalid" {
		t.Fatalf("invalid reservation target mapped=%t error=%v", mapped, err)
	}
	nested := httptest.NewRequest(http.MethodPost, reservationPrefix+reservationID.String()+"/nested/commit", nil)
	if _, mapped, err := resolveAdmissionAuthorization(nested); err != nil || mapped {
		t.Fatalf("nested route mapped=%t error=%v", mapped, err)
	}
	unknown := httptest.NewRequest(http.MethodGet, "/v1/unknown", nil)
	if _, mapped, err := resolveAdmissionAuthorization(unknown); err != nil || mapped {
		t.Fatalf("unknown mapped=%t error=%v", mapped, err)
	}
}

func TestAdmissionHTTPAuthenticationAuthorizationAndOwnershipFailClosed(t *testing.T) {
	fixture := newAdmissionHTTPFixture(t)
	handler := fixture.guardedHandler(t)
	payload, err := json.Marshal(fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{"Content-Type": {"application/json"}, httpx.HeaderIdempotencyKey: {"guarded-request-0001"}}
	missing := callAdmissionHTTP(t, handler, http.MethodPost, reservationsPath, payload, headers, nil)
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("missing credential status=%d body=%s", missing.Code, missing.Body.String())
	}

	deniedHeaders := headers.Clone()
	deniedHeaders.Set("Authorization", "Bearer denied-token")
	denied := callAdmissionHTTP(t, handler, http.MethodPost, reservationsPath, payload, deniedHeaders, nil)
	if denied.Code != http.StatusForbidden || admissionProblem(t, denied).Reason != "permission_not_granted" {
		t.Fatalf("missing permission status=%d body=%s", denied.Code, denied.Body.String())
	}

	wrongWorkspace := fixture.request
	wrongWorkspace.Workspace = "other-team"
	wrongWorkspacePayload, err := json.Marshal(wrongWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	wrongWorkspaceHeaders := headers.Clone()
	wrongWorkspaceHeaders.Set(httpx.HeaderIdempotencyKey, "guarded-request-0002")
	wrongWorkspaceHeaders.Set("Authorization", "Bearer owner-token")
	workspaceDenied := callAdmissionHTTP(t, handler, http.MethodPost, reservationsPath, wrongWorkspacePayload, wrongWorkspaceHeaders, nil)
	if workspaceDenied.Code != http.StatusNotFound || admissionProblem(t, workspaceDenied).Reason != "entitlement_not_found" {
		t.Fatalf("cross-workspace status=%d body=%s", workspaceDenied.Code, workspaceDenied.Body.String())
	}

	allowedHeaders := headers.Clone()
	allowedHeaders.Set("Authorization", "Bearer owner-token")
	created := callAdmissionHTTP(t, handler, http.MethodPost, reservationsPath, payload, allowedHeaders, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("allowed create status=%d body=%s", created.Code, created.Body.String())
	}
	var decision admission.Decision
	if err := json.Unmarshal(created.Body.Bytes(), &decision); err != nil {
		t.Fatal(err)
	}
	finalizePayload, err := json.Marshal(finalizeHTTPRequest{RequestDigest: fixture.request.RequestDigest})
	if err != nil {
		t.Fatal(err)
	}
	foreignHeaders := http.Header{
		"Content-Type":  {"application/json"},
		"Authorization": {"Bearer foreign-token"},
		"If-Match":      {decision.Reservation.Version.ETag()},
	}
	releasePath := reservationPrefix + decision.Reservation.ID.String() + "/release"
	foreign := callAdmissionHTTP(t, handler, http.MethodPost, releasePath, finalizePayload, foreignHeaders, nil)
	if foreign.Code != http.StatusForbidden || admissionProblem(t, foreign).Reason != "reservation_subject_mismatch" {
		t.Fatalf("foreign finalization status=%d body=%s", foreign.Code, foreign.Body.String())
	}
	stored, err := fixture.repository.Get(context.Background(), decision.Reservation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != admission.ReservationReserved {
		t.Fatalf("foreign authorized caller changed reservation state to %s", stored.State)
	}
}

func TestAdmissionHTTPRejectsAmbiguousConditionalHeaders(t *testing.T) {
	fixture := newAdmissionHTTPFixture(t)
	created := fixture.call(t, http.MethodPost, reservationsPath, fixture.request,
		map[string]string{httpx.HeaderIdempotencyKey: "conditional-request-0001"}, fixture.principal)
	var decision admission.Decision
	if err := json.Unmarshal(created.Body.Bytes(), &decision); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(finalizeHTTPRequest{RequestDigest: fixture.request.RequestDigest})
	if err != nil {
		t.Fatal(err)
	}
	path := reservationPrefix + decision.Reservation.ID.String() + "/release"
	for _, header := range []string{"If-Match", "If-None-Match"} {
		t.Run(header, func(t *testing.T) {
			headers := http.Header{"Content-Type": {"application/json"}}
			value := decision.Reservation.Version.ETag()
			if header == "If-None-Match" {
				value = "*"
			}
			headers.Add(header, value)
			headers.Add(header, value)
			response := callAdmissionHTTP(t, fixture.handler, http.MethodPost, path, payload, headers, &fixture.principal)
			if response.Code != http.StatusBadRequest || admissionProblem(t, response).Reason != "conditional_header_ambiguous" {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	stored, err := fixture.repository.Get(context.Background(), decision.Reservation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != admission.ReservationReserved {
		t.Fatalf("ambiguous precondition changed reservation state to %s", stored.State)
	}
}

func TestAdmissionHTTPRejectsAmbiguousIdempotencyHeaderBeforeMutation(t *testing.T) {
	fixture := newAdmissionHTTPFixture(t)
	payload, err := json.Marshal(fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{"Content-Type": {"application/json"}}
	headers.Add(httpx.HeaderIdempotencyKey, "ambiguous-idempotency-0001")
	headers.Add(httpx.HeaderIdempotencyKey, "ambiguous-idempotency-0001")
	response := callAdmissionHTTP(t, fixture.handler, http.MethodPost, reservationsPath, payload, headers, &fixture.principal)
	if response.Code != http.StatusBadRequest || admissionProblem(t, response).Reason != "idempotency_key_required" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	validHeaders := http.Header{
		"Content-Type":             {"application/json"},
		httpx.HeaderIdempotencyKey: {"ambiguous-idempotency-0001"},
	}
	created := callAdmissionHTTP(t, fixture.handler, http.MethodPost, reservationsPath, payload, validHeaders, &fixture.principal)
	if created.Code != http.StatusCreated {
		t.Fatalf("ambiguous header consumed idempotency key: status=%d body=%s", created.Code, created.Body.String())
	}
}

func TestAdmissionHTTPRejectsInvalidTransportBodiesBeforeMutation(t *testing.T) {
	fixture := newAdmissionHTTPFixture(t)
	validPayload, err := json.Marshal(fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	unknownPayload := append([]byte{}, validPayload[:len(validPayload)-1]...)
	unknownPayload = append(unknownPayload, []byte(`,"unexpected":true}`)...)
	oversizedRequest := fixture.request
	oversizedRequest.Workspace = string(bytes.Repeat([]byte{'w'}, maximumAdmissionBody))
	oversizedPayload, err := json.Marshal(oversizedRequest)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, contentType, reason, key string
		payload                        []byte
		status                         int
	}{
		{name: "unsupported content type", contentType: "text/plain", payload: validPayload, status: http.StatusBadRequest, reason: "unsupported_media_type", key: "transport-negative-0001"},
		{name: "unknown field", contentType: "application/json", payload: unknownPayload, status: http.StatusBadRequest, reason: "invalid_json", key: "transport-negative-0002"},
		{name: "multiple values", contentType: "application/json", payload: append(append([]byte{}, validPayload...), validPayload...), status: http.StatusBadRequest, reason: "multiple_json_values", key: "transport-negative-0003"},
		{name: "oversized body", contentType: "application/json", payload: oversizedPayload, status: http.StatusTooManyRequests, reason: "request_body_too_large", key: "transport-negative-0004"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			headers := http.Header{"Content-Type": {testCase.contentType}, httpx.HeaderIdempotencyKey: {testCase.key}}
			response := callAdmissionHTTP(t, fixture.handler, http.MethodPost, reservationsPath, testCase.payload, headers, &fixture.principal)
			if response.Code != testCase.status || admissionProblem(t, response).Reason != testCase.reason {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			validHeaders := http.Header{"Content-Type": {"application/json"}, httpx.HeaderIdempotencyKey: {testCase.key}}
			created := callAdmissionHTTP(t, fixture.handler, http.MethodPost, reservationsPath, validPayload, validHeaders, &fixture.principal)
			if created.Code != http.StatusCreated {
				t.Fatalf("invalid body consumed idempotency key: status=%d body=%s", created.Code, created.Body.String())
			}
		})
	}
}

func TestAdmissionMetricsExcludeUnauthenticatedAndMalformedRequests(t *testing.T) {
	metrics, endpoint := newRunningAdmissionMetrics(t)
	fixture := newAdmissionHTTPFixtureWithMetrics(t, metrics)
	handler := metrics.Middleware(fixture.guardedHandler(t))
	payload, err := json.Marshal(fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	baseHeaders := http.Header{
		"Content-Type":             {"application/json"},
		httpx.HeaderIdempotencyKey: {"metrics-exclusion-0001"},
	}
	unauthenticated := callAdmissionHTTP(t, handler, http.MethodPost, reservationsPath, payload, baseHeaders, nil)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d body=%s", unauthenticated.Code, unauthenticated.Body.String())
	}
	deniedHeaders := baseHeaders.Clone()
	deniedHeaders.Set("Authorization", "Bearer denied-token")
	denied := callAdmissionHTTP(t, handler, http.MethodPost, reservationsPath, payload, deniedHeaders, nil)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("unauthorized status=%d body=%s", denied.Code, denied.Body.String())
	}
	malformedHeaders := baseHeaders.Clone()
	malformedHeaders.Set("Authorization", "Bearer owner-token")
	malformed := callAdmissionHTTP(t, handler, http.MethodPost, reservationsPath, []byte(`{"broken":`), malformedHeaders, nil)
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed status=%d body=%s", malformed.Code, malformed.Body.String())
	}

	// A structurally valid but semantically invalid request is inside the SLI,
	// proving the two exclusions above did not merely disable instrumentation.
	semantic := fixture.request
	semantic.TTLSeconds = 0
	semanticPayload, err := json.Marshal(semantic)
	if err != nil {
		t.Fatal(err)
	}
	semanticHeaders := malformedHeaders.Clone()
	semanticHeaders.Set(httpx.HeaderIdempotencyKey, "metrics-exclusion-0002")
	invalid := callAdmissionHTTP(t, handler, http.MethodPost, reservationsPath, semanticPayload, semanticHeaders, nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("semantic invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	body := readAdmissionMetrics(t, endpoint)
	if got := admissionDecisionTotal(t, body, "admit"); got != 1 {
		t.Fatalf("admit decisions = %v, want only the semantic invalid request\n%s", got, body)
	}
	if !strings.Contains(body, `mindclade_control_admission_decisions_total{operation="admit",result="invalid"} 1`) {
		t.Fatalf("semantic invalid decision missing:\n%s", body)
	}
	if !strings.Contains(body, `mindclade_control_admission_decision_duration_seconds_count{operation="admit"} 1`) {
		t.Fatalf("excluded requests contributed latency or valid request was omitted:\n%s", body)
	}
}

func TestAdmissionMetricsIncludeParsingAndSerializationBoundaryLatency(t *testing.T) {
	metrics, endpoint := newRunningAdmissionMetrics(t)
	fixture := newAdmissionHTTPFixtureWithMetrics(t, metrics)
	handler := metrics.Middleware(fixture.guardedHandler(t))
	payload, err := json.Marshal(fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, reservationsPath, &delayedAdmissionReader{
		reader: bytes.NewReader(payload),
		delay:  20 * time.Millisecond,
	})
	request.Header = http.Header{
		"Content-Type":             {"application/json"},
		"Authorization":            {"Bearer owner-token"},
		httpx.HeaderIdempotencyKey: {"metrics-boundary-0001"},
	}
	response := &delayedAdmissionWriter{ResponseRecorder: httptest.NewRecorder(), delay: 20 * time.Millisecond}
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	body := readAdmissionMetrics(t, endpoint)
	seconds := admissionMetricValue(t, body, `mindclade_control_admission_decision_duration_seconds_sum\{operation="admit"\}`)
	if seconds < 0.035 {
		t.Fatalf("boundary duration=%f, want request parsing and response serialization included", seconds)
	}
}

func TestAdmissionMetricsSeparateCallerCancellationAndServerDeadline(t *testing.T) {
	cases := []struct {
		name          string
		terminal      error
		status        int
		result        string
		durationCount string
	}{
		{name: "caller canceled", terminal: context.Canceled, status: http.StatusRequestTimeout, result: "canceled", durationCount: "0"},
		{name: "server deadline", terminal: context.DeadlineExceeded, status: http.StatusGatewayTimeout, result: "deadline", durationCount: "1"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			metrics, endpoint := newRunningAdmissionMetrics(t)
			fixture := newAdmissionHTTPFixture(t)
			mux, err := newAdmissionMux(&terminalAdmissionEngine{err: test.terminal}, metrics)
			if err != nil {
				t.Fatal(err)
			}
			fixture.handler = transport.Preconditions()(mux)
			handler := metrics.Middleware(fixture.guardedHandler(t))
			payload, err := json.Marshal(fixture.request)
			if err != nil {
				t.Fatal(err)
			}
			headers := http.Header{
				"Content-Type":             {"application/json"},
				"Authorization":            {"Bearer owner-token"},
				httpx.HeaderIdempotencyKey: {"metrics-terminal-0001"},
			}
			response := callAdmissionHTTP(t, handler, http.MethodPost, reservationsPath, payload, headers, nil)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			body := readAdmissionMetrics(t, endpoint)
			want := fmt.Sprintf(`mindclade_control_admission_decisions_total{operation="admit",result=%q} 1`, test.result)
			if !strings.Contains(body, want) {
				t.Fatalf("terminal result missing %q:\n%s", want, body)
			}
			count := fmt.Sprintf(`mindclade_control_admission_decision_duration_seconds_count{operation="admit"} %s`, test.durationCount)
			if !strings.Contains(body, count) {
				t.Fatalf("duration classification missing %q:\n%s", count, body)
			}
		})
	}
}

func TestAdmissionMetricsClassifyTerminalResponseWriteFailure(t *testing.T) {
	metrics, endpoint := newRunningAdmissionMetrics(t)
	fixture := newAdmissionHTTPFixtureWithMetrics(t, metrics)
	handler := metrics.Middleware(fixture.guardedHandler(t))
	payload, err := json.Marshal(fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, reservationsPath, bytes.NewReader(payload))
	request.Header = http.Header{
		"Content-Type":             {"application/json"},
		"Authorization":            {"Bearer owner-token"},
		httpx.HeaderIdempotencyKey: {"metrics-response-failure-0001"},
	}
	handler.ServeHTTP(&failingAdmissionWriter{header: make(http.Header)}, request)
	body := readAdmissionMetrics(t, endpoint)
	if !strings.Contains(body, `mindclade_control_admission_decisions_total{operation="admit",result="unavailable"} 1`) ||
		!strings.Contains(body, `mindclade_control_admission_decisions_total{operation="admit",result="allow"} 0`) {
		t.Fatalf("terminal response failure was misclassified:\n%s", body)
	}
}

func TestAdmissionMetricsResponseWriteFailureSupersedesSemanticFailure(t *testing.T) {
	metrics, endpoint := newRunningAdmissionMetrics(t)
	fixture := newAdmissionHTTPFixture(t)
	semantic := faults.New(faults.CodeConflict, "engine conflict")
	mux, err := newAdmissionMux(&terminalAdmissionEngine{err: semantic}, metrics)
	if err != nil {
		t.Fatal(err)
	}
	fixture.handler = transport.Preconditions()(mux)
	handler := metrics.Middleware(fixture.guardedHandler(t))
	payload, err := json.Marshal(fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, reservationsPath, bytes.NewReader(payload))
	request.Header = http.Header{
		"Content-Type":             {"application/json"},
		"Authorization":            {"Bearer owner-token"},
		httpx.HeaderIdempotencyKey: {"metrics-response-failure-0002"},
	}
	handler.ServeHTTP(&failingAdmissionWriter{header: make(http.Header)}, request)
	body := readAdmissionMetrics(t, endpoint)
	if !strings.Contains(body, `mindclade_control_admission_decisions_total{operation="admit",result="unavailable"} 1`) ||
		!strings.Contains(body, `mindclade_control_admission_decisions_total{operation="admit",result="conflict"} 0`) {
		t.Fatalf("response failure did not supersede semantic conflict:\n%s", body)
	}
}

type terminalAdmissionEngine struct {
	err error
}

func (engine *terminalAdmissionEngine) Admit(context.Context, admission.AdmitRequest) (admission.Decision, error) {
	return admission.Decision{}, engine.err
}

func (engine *terminalAdmissionEngine) Commit(context.Context, identifiers.ID, resourceversion.Version, identifiers.Digest, string, admission.Quota) (admission.Decision, error) {
	return admission.Decision{}, engine.err
}

func (engine *terminalAdmissionEngine) Release(context.Context, identifiers.ID, resourceversion.Version, identifiers.Digest, string) (admission.Decision, error) {
	return admission.Decision{}, engine.err
}

type delayedAdmissionReader struct {
	reader io.Reader
	delay  time.Duration
	once   sync.Once
}

func (reader *delayedAdmissionReader) Read(value []byte) (int, error) {
	reader.once.Do(func() { time.Sleep(reader.delay) })
	return reader.reader.Read(value)
}

type delayedAdmissionWriter struct {
	*httptest.ResponseRecorder
	delay time.Duration
	once  sync.Once
}

func (writer *delayedAdmissionWriter) Write(value []byte) (int, error) {
	writer.once.Do(func() { time.Sleep(writer.delay) })
	return writer.ResponseRecorder.Write(value)
}

type failingAdmissionWriter struct {
	header http.Header
	status int
}

func (writer *failingAdmissionWriter) Header() http.Header { return writer.header }
func (writer *failingAdmissionWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
}
func (*failingAdmissionWriter) Write([]byte) (int, error) {
	return 0, errors.New("terminal response write failed")
}

func newRunningAdmissionMetrics(t *testing.T) (*admissionmetrics.Runtime, string) {
	t.Helper()
	runtime, err := admissionmetrics.New("127.0.0.1:0", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	component := runtime.Component()
	if err := component.Start(context.Background()); err != nil {
		_ = runtime.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- component.Run(context.Background()) }()
	deadline := time.Now().Add(time.Second)
	for component.Readiness(context.Background()) != nil {
		if time.Now().After(deadline) {
			_ = runtime.Close()
			t.Fatal("admission metrics listener did not become ready")
		}
		time.Sleep(time.Millisecond)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := component.Stop(ctx); err != nil {
			t.Errorf("stop admission metrics: %v", err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("run admission metrics: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("admission metrics listener did not stop")
		}
		if err := runtime.Close(); err != nil {
			t.Errorf("close admission metrics: %v", err)
		}
	})
	return runtime, "http://" + runtime.Address().String() + "/metrics"
}

func readAdmissionMetrics(t *testing.T, endpoint string) string {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	response, err := client.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("metrics status=%d body=%s", response.StatusCode, body)
	}
	return string(body)
}

func admissionDecisionTotal(t *testing.T, body, operation string) float64 {
	t.Helper()
	pattern := regexp.MustCompile(`(?m)^mindclade_control_admission_decisions_total\{operation="` + regexp.QuoteMeta(operation) + `",result="[a-z_]+"\} ([0-9.eE+-]+)$`)
	var total float64
	for _, match := range pattern.FindAllStringSubmatch(body, -1) {
		value, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			t.Fatal(err)
		}
		total += value
	}
	return total
}

func admissionMetricValue(t *testing.T, body, metricPattern string) float64 {
	t.Helper()
	match := regexp.MustCompile(metricPattern + ` ([0-9.eE+-]+)`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("metric %s missing:\n%s", metricPattern, body)
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
