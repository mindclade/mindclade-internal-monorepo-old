// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package api

import (
	"bytes"
	"context"
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
	handler, err := newAdmissionMux(service)
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
	unknown := httptest.NewRequest(http.MethodGet, "/v1/unknown", nil)
	if _, mapped, err := resolveAdmissionAuthorization(unknown); err != nil || mapped {
		t.Fatalf("unknown mapped=%t error=%v", mapped, err)
	}
}
