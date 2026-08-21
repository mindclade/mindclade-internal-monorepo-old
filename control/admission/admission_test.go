// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admission

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

var testStart = time.Date(2026, time.August, 21, 6, 0, 0, 0, time.UTC)

type fixture struct {
	clock      *clock.FakeClock
	repository *MemoryRepository
	service    Service
	route      GatewayRoute
}

func newFixture(t *testing.T, requestLimit uint64) fixture {
	t.Helper()
	fake := clock.NewFake(testStart)
	repository := NewMemoryRepository(1_000)
	route := GatewayRoute{Endpoint: "chat-primary", Provider: "vertex", Model: "gemini-pro"}
	entitlement := Entitlement{
		ID:             testID(t, "entitlement", testStart),
		Subject:        "service-account",
		Workspace:      "research-team",
		PolicyEpoch:    7,
		Routes:         []GatewayRoute{route},
		MaximumRequest: Quota{UnitRequests: 1, UnitInputTokens: 2_000, UnitOutputTokens: 1_000, UnitCostMicros: 5_000},
		NotBefore:      testStart.Add(-time.Minute),
		ExpiresAt:      testStart.Add(time.Hour),
		Version:        testVersion(t, 1, "entitlement-v1"),
	}
	budget := Budget{
		ID:        testID(t, "budget", testStart.Add(time.Millisecond)),
		Workspace: "research-team",
		Limit:     Quota{UnitRequests: requestLimit, UnitInputTokens: 20_000, UnitOutputTokens: 10_000, UnitCostMicros: 50_000},
		StartsAt:  testStart.Add(-time.Minute),
		ExpiresAt: testStart.Add(time.Hour),
		Version:   testVersion(t, 1, "budget-v1"),
	}
	if err := repository.PutEntitlement(context.Background(), entitlement); err != nil {
		t.Fatal(err)
	}
	if err := repository.PutBudget(context.Background(), budget); err != nil {
		t.Fatal(err)
	}
	return fixture{
		clock:      fake,
		repository: repository,
		service:    Service{Repository: repository, Clock: fake, MaximumTTL: time.Minute},
		route:      route,
	}
}

func testID(t *testing.T, kind string, at time.Time) identifiers.ID {
	t.Helper()
	id, err := identifiers.NewIDAt(identifiers.MustParseKind(kind), at)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testVersion(t *testing.T, generation uint64, value string) resourceversion.Version {
	t.Helper()
	version, err := resourceversion.New(generation, identifiers.SHA256String(value))
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func request(sequence int) AdmitRequest {
	return AdmitRequest{
		Idempotency: idempotency.Identity{
			Scope: idempotency.MustParseScope("research-team/mlflow-gateway/service-account"),
			Key:   idempotency.MustParseKey(fmt.Sprintf("request-%04d", sequence)),
		},
		RequestDigest: identifiers.SHA256String(fmt.Sprintf("payload-%04d", sequence)),
		Subject:       "service-account",
		Workspace:     "research-team",
		Route:         GatewayRoute{Endpoint: "chat-primary", Provider: "vertex", Model: "gemini-pro"},
		PolicyEpoch:   7,
		Requested:     Quota{UnitRequests: 1, UnitInputTokens: 100, UnitOutputTokens: 50, UnitCostMicros: 500},
		TTL:           30 * time.Second,
	}
}

func TestAdmissionReservationCommitAndIdempotentReplay(t *testing.T) {
	fixture := newFixture(t, 2)
	ctx := context.Background()
	first, err := fixture.service.Admit(ctx, request(1))
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || first.Reservation.State != ReservationReserved {
		t.Fatalf("unexpected first decision: %+v", first)
	}
	replay, err := fixture.service.Admit(ctx, request(1))
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || replay.Reservation.ID.String() != first.Reservation.ID.String() {
		t.Fatal("idempotent admission did not replay the original reservation")
	}

	actual := Quota{UnitRequests: 1, UnitInputTokens: 80, UnitOutputTokens: 40, UnitCostMicros: 400}
	committed, err := fixture.service.Commit(ctx, first.Reservation.ID, first.Reservation.Version, first.Reservation.RequestDigest, actual)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Replayed || committed.Reservation.State != ReservationCommitted || !committed.Reservation.Actual.Equal(actual) {
		t.Fatalf("unexpected commit: %+v", committed)
	}
	commitReplay, err := fixture.service.Commit(ctx, first.Reservation.ID, first.Reservation.Version, first.Reservation.RequestDigest, actual)
	if err != nil {
		t.Fatal(err)
	}
	if !commitReplay.Replayed || commitReplay.Reservation.Version.String() != committed.Reservation.Version.String() {
		t.Fatal("commit retry did not replay the terminal result")
	}

	committed.Reservation.Actual[UnitCostMicros] = 999
	stored, err := fixture.repository.Get(ctx, first.Reservation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Actual[UnitCostMicros] != 400 {
		t.Fatal("caller mutation corrupted repository state")
	}
}

func TestIdempotencyKeyCannotChangeRequest(t *testing.T) {
	fixture := newFixture(t, 2)
	ctx := context.Background()
	original := request(1)
	if _, err := fixture.service.Admit(ctx, original); err != nil {
		t.Fatal(err)
	}
	changed := original
	changed.RequestDigest = identifiers.SHA256String("different")
	if _, err := fixture.service.Admit(ctx, changed); !faults.IsReason(err, "idempotency_payload_mismatch") {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestAdmissionFailsClosedOnRouteEpochAndRequestMaximum(t *testing.T) {
	for name, testCase := range map[string]struct {
		mutate func(*AdmitRequest)
		reason string
	}{
		"route":   {func(value *AdmitRequest) { value.Route.Model = "unapproved" }, "route_not_entitled"},
		"epoch":   {func(value *AdmitRequest) { value.PolicyEpoch-- }, "policy_epoch_stale"},
		"maximum": {func(value *AdmitRequest) { value.Requested[UnitCostMicros] = 5_001 }, "request_exceeds_entitlement"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newFixture(t, 2)
			candidate := request(1)
			testCase.mutate(&candidate)
			if _, err := fixture.service.Admit(context.Background(), candidate); !faults.IsReason(err, testCase.reason) {
				t.Fatalf("expected %s, got %v", testCase.reason, err)
			}
		})
	}
}

func TestConcurrentReservationsCannotOverspendBudget(t *testing.T) {
	fixture := newFixture(t, 10)
	const callers = 64
	requests := make([]AdmitRequest, callers)
	for index := range requests {
		requests[index] = request(index + 1)
	}
	var admitted atomic.Int64
	errorsByCode := make(chan faults.Code, callers)
	var wait sync.WaitGroup
	for index := range requests {
		wait.Add(1)
		go func(candidate AdmitRequest) {
			defer wait.Done()
			if _, err := fixture.service.Admit(context.Background(), candidate); err != nil {
				errorsByCode <- faults.CodeOf(err)
				return
			}
			admitted.Add(1)
		}(requests[index])
	}
	wait.Wait()
	close(errorsByCode)
	if admitted.Load() != 10 {
		t.Fatalf("admitted %d requests, want 10", admitted.Load())
	}
	for code := range errorsByCode {
		if code != faults.CodeResourceExhausted {
			t.Fatalf("unexpected rejection code: %s", code)
		}
	}
}

func TestReleaseAndExpirationReturnReservedCapacity(t *testing.T) {
	fixture := newFixture(t, 1)
	ctx := context.Background()
	first, err := fixture.service.Admit(ctx, request(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Admit(ctx, request(2)); !faults.IsReason(err, "budget_exhausted") {
		t.Fatalf("expected exhausted budget, got %v", err)
	}
	released, err := fixture.service.Release(ctx, first.Reservation.ID, first.Reservation.Version, first.Reservation.RequestDigest)
	if err != nil || released.Reservation.State != ReservationReleased {
		t.Fatalf("release failed: decision=%+v error=%v", released, err)
	}
	second, err := fixture.service.Admit(ctx, request(2))
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.clock.Advance(31 * time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Commit(ctx, second.Reservation.ID, second.Reservation.Version, second.Reservation.RequestDigest, Quota{UnitRequests: 1}); !faults.IsReason(err, "reservation_expired") {
		t.Fatalf("expected expiration, got %v", err)
	}
	if _, err := fixture.service.Admit(ctx, request(3)); err != nil {
		t.Fatalf("expired reservation did not return capacity: %v", err)
	}
}

func TestFinalizationRejectsStaleVersionForeignDigestAndOverspend(t *testing.T) {
	fixture := newFixture(t, 1)
	ctx := context.Background()
	decision, err := fixture.service.Admit(ctx, request(1))
	if err != nil {
		t.Fatal(err)
	}
	stale := testVersion(t, 99, "stale")
	if _, err := fixture.service.Commit(ctx, decision.Reservation.ID, stale, decision.Reservation.RequestDigest, nil); !faults.IsReason(err, "reservation_version_stale") {
		t.Fatalf("expected stale version, got %v", err)
	}
	if _, err := fixture.service.Commit(ctx, decision.Reservation.ID, decision.Reservation.Version, identifiers.SHA256String("foreign"), nil); !faults.IsReason(err, "request_digest_mismatch") {
		t.Fatalf("expected foreign digest rejection, got %v", err)
	}
	actual := Quota{UnitRequests: 2}
	if _, err := fixture.service.Commit(ctx, decision.Reservation.ID, decision.Reservation.Version, decision.Reservation.RequestDigest, actual); !faults.IsReason(err, "actual_exceeds_reservation") {
		t.Fatalf("expected actual-usage rejection, got %v", err)
	}
}

func TestReservationTransitionsAreValidatedAndVersioned(t *testing.T) {
	fixture := newFixture(t, 2)
	decision, err := fixture.service.Admit(context.Background(), request(1))
	if err != nil {
		t.Fatal(err)
	}
	reservation := decision.Reservation
	now := fixture.clock.Now()
	if _, err := reservation.Expire(now); !faults.IsReason(err, "reservation_not_expired") {
		t.Fatalf("expected early-expiration rejection, got %v", err)
	}
	committed, err := reservation.Commit(Quota{UnitRequests: 1}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if committed.State != ReservationCommitted || committed.Version.Generation() != reservation.Version.Generation()+1 {
		t.Fatalf("commit did not create the next terminal version: %+v", committed)
	}
	if _, err := committed.Release(now.Add(2 * time.Second)); !faults.IsReason(err, "reservation_terminal") {
		t.Fatalf("expected terminal transition rejection, got %v", err)
	}
}
