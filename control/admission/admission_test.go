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
	}
	var err error
	entitlement, err = entitlement.Seal(1)
	if err != nil {
		t.Fatal(err)
	}
	budget := Budget{
		ID:        testID(t, "budget", testStart.Add(time.Millisecond)),
		Workspace: "research-team",
		Limit:     Quota{UnitRequests: requestLimit, UnitInputTokens: 20_000, UnitOutputTokens: 10_000, UnitCostMicros: 50_000},
		StartsAt:  testStart.Add(-time.Minute),
		ExpiresAt: testStart.Add(time.Hour),
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

func candidateFor(t *testing.T, fixture fixture, sequence int) (PolicySnapshot, Reservation) {
	t.Helper()
	snapshot, err := fixture.repository.Snapshot(context.Background(), "service-account", "research-team")
	if err != nil {
		t.Fatal(err)
	}
	admit := request(sequence)
	now := fixture.clock.Now().Round(0).UTC()
	expiresAt := now.Add(admit.TTL)
	if snapshot.Entitlement.ExpiresAt.Before(expiresAt) {
		expiresAt = snapshot.Entitlement.ExpiresAt
	}
	if snapshot.Budget.ExpiresAt.Before(expiresAt) {
		expiresAt = snapshot.Budget.ExpiresAt
	}
	candidate := Reservation{
		ID:                 testID(t, "reservation", now),
		Idempotency:        admit.Idempotency,
		RequestDigest:      admit.RequestDigest,
		Subject:            admit.Subject,
		Workspace:          admit.Workspace,
		Route:              admit.Route,
		PolicyEpoch:        admit.PolicyEpoch,
		EntitlementID:      snapshot.Entitlement.ID,
		EntitlementVersion: snapshot.Entitlement.Version,
		BudgetID:           snapshot.Budget.ID,
		BudgetVersion:      snapshot.Budget.Version,
		Reserved:           snapshot.Entitlement.MaximumRequest.Clone(),
		RequestedTTL:       admit.TTL,
		State:              ReservationReserved,
		CreatedAt:          now,
		ExpiresAt:          expiresAt,
	}
	candidate, err = versionReservation(candidate, 1)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, candidate
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
	dispatched, err := fixture.service.Dispatch(ctx, first.Reservation.ID, first.Reservation.Version, first.Reservation.RequestDigest, "service-account")
	if err != nil || dispatched.Reservation.State != ReservationDispatched {
		t.Fatalf("dispatch failed: decision=%+v error=%v", dispatched, err)
	}
	committed, err := fixture.service.Commit(ctx, first.Reservation.ID, dispatched.Reservation.Version, first.Reservation.RequestDigest, "service-account", actual)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Replayed || committed.Reservation.State != ReservationCommitted || !committed.Reservation.Actual.Equal(actual) {
		t.Fatalf("unexpected commit: %+v", committed)
	}
	commitReplay, err := fixture.service.Commit(ctx, first.Reservation.ID, dispatched.Reservation.Version, first.Reservation.RequestDigest, "service-account", actual)
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

func TestAmbiguousDispatchIsDurablyMaxCharged(t *testing.T) {
	fixture := newFixture(t, 1)
	ctx := context.Background()
	decision, err := fixture.service.Admit(ctx, request(1))
	if err != nil {
		t.Fatal(err)
	}
	dispatched, err := fixture.service.Dispatch(ctx, decision.Reservation.ID, decision.Reservation.Version,
		decision.Reservation.RequestDigest, "service-account")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Release(ctx, decision.Reservation.ID, dispatched.Reservation.Version,
		decision.Reservation.RequestDigest, "service-account"); !faults.IsReason(err, "reservation_already_dispatched") {
		t.Fatalf("dispatched work was releasable: %v", err)
	}
	pending, err := fixture.service.MarkReconciliationPending(ctx, decision.Reservation.ID, dispatched.Reservation.Version,
		decision.Reservation.RequestDigest, "service-account")
	if err != nil || pending.Reservation.State != ReservationReconciliationPending || pending.Reservation.Version.Generation() != 3 {
		t.Fatalf("pending=%+v error=%v", pending, err)
	}
	terminal, err := fixture.service.Reconcile(ctx, decision.Reservation.ID, pending.Reservation.Version,
		decision.Reservation.RequestDigest, "service-account")
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Reservation.State != ReservationCommitted || terminal.Reservation.Version.Generation() != 4 ||
		!terminal.Reservation.Actual.Equal(terminal.Reservation.Reserved) {
		t.Fatalf("ambiguous dispatch was not max-charged: %+v", terminal.Reservation)
	}
	if _, err := fixture.service.Admit(ctx, request(2)); !faults.IsReason(err, "budget_exhausted") {
		t.Fatalf("max charge did not remain in budget ledger: %v", err)
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

func TestIdempotencyBindsRequestedTTL(t *testing.T) {
	fixture := newFixture(t, 2)
	ctx := context.Background()
	original := request(1)
	if _, err := fixture.service.Admit(ctx, original); err != nil {
		t.Fatal(err)
	}
	changed := original
	changed.TTL++
	if _, err := fixture.service.Admit(ctx, changed); !faults.IsReason(err, "idempotency_payload_mismatch") {
		t.Fatalf("expected TTL idempotency conflict, got %v", err)
	}
}

func TestExpiredIdempotentAdmissionFailsClosed(t *testing.T) {
	fixture := newFixture(t, 2)
	ctx := context.Background()
	original, err := fixture.service.Admit(ctx, request(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.clock.Advance(original.Reservation.RequestedTTL); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Admit(ctx, request(1)); !faults.IsReason(err, "reservation_expired") {
		t.Fatalf("expected exact-boundary expiration, got %v", err)
	}
	stored, err := fixture.repository.Get(ctx, original.Reservation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != ReservationExpired || stored.FinalizedAt.Before(stored.ExpiresAt) {
		t.Fatalf("expired replay was not persisted atomically: %+v", stored)
	}
	if _, err := fixture.service.Admit(ctx, request(1)); !faults.IsReason(err, "reservation_expired") {
		t.Fatalf("expected terminal expiration replay to fail closed, got %v", err)
	}
}

func TestTerminalAdmissionReplayReturnsOriginalDecision(t *testing.T) {
	for name, finalize := range map[string]func(fixture, Decision) error{
		"committed": func(fixture fixture, decision Decision) error {
			dispatched, err := fixture.service.Dispatch(context.Background(), decision.Reservation.ID, decision.Reservation.Version, decision.Reservation.RequestDigest, "service-account")
			if err != nil {
				return err
			}
			_, err = fixture.service.Commit(context.Background(), decision.Reservation.ID, dispatched.Reservation.Version, decision.Reservation.RequestDigest, "service-account", Quota{UnitRequests: 1})
			return err
		},
		"released": func(fixture fixture, decision Decision) error {
			_, err := fixture.service.Release(context.Background(), decision.Reservation.ID, decision.Reservation.Version, decision.Reservation.RequestDigest, "service-account")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newFixture(t, 2)
			original, err := fixture.service.Admit(context.Background(), request(1))
			if err != nil {
				t.Fatal(err)
			}
			if err := finalize(fixture, original); err != nil {
				t.Fatal(err)
			}
			replay, err := fixture.service.Admit(context.Background(), request(1))
			if err != nil {
				t.Fatal(err)
			}
			if !replay.Replayed || replay.Reservation.State == ReservationReserved {
				t.Fatalf("terminal replay returned a new authorization: %+v", replay)
			}
		})
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
	released, err := fixture.service.Release(ctx, first.Reservation.ID, first.Reservation.Version, first.Reservation.RequestDigest, "service-account")
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
	if _, err := fixture.service.Dispatch(ctx, second.Reservation.ID, second.Reservation.Version, second.Reservation.RequestDigest, "service-account"); !faults.IsReason(err, "reservation_expired") {
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
	validActual := Quota{UnitRequests: 1}
	if _, err := fixture.service.Commit(ctx, decision.Reservation.ID, stale, decision.Reservation.RequestDigest, "service-account", validActual); !faults.IsReason(err, "reservation_version_stale") {
		t.Fatalf("expected stale version, got %v", err)
	}
	if _, err := fixture.service.Commit(ctx, decision.Reservation.ID, decision.Reservation.Version, identifiers.SHA256String("foreign"), "service-account", validActual); !faults.IsReason(err, "request_digest_mismatch") {
		t.Fatalf("expected foreign digest rejection, got %v", err)
	}
	dispatched, err := fixture.service.Dispatch(ctx, decision.Reservation.ID, decision.Reservation.Version, decision.Reservation.RequestDigest, "service-account")
	if err != nil {
		t.Fatal(err)
	}
	actual := Quota{UnitRequests: 1, UnitInputTokens: 2_001}
	if _, err := fixture.service.Commit(ctx, decision.Reservation.ID, dispatched.Reservation.Version, decision.Reservation.RequestDigest, "service-account", actual); !faults.IsReason(err, "actual_exceeds_reservation") {
		t.Fatalf("expected actual-usage rejection, got %v", err)
	}
}

func TestRepositoryBindsReservationToPolicySnapshot(t *testing.T) {
	for name, mutate := range map[string]func(*Reservation){
		"entitlement id": func(candidate *Reservation) {
			candidate.EntitlementID = testID(t, "entitlement", testStart.Add(time.Second))
		},
		"entitlement version": func(candidate *Reservation) {
			candidate.EntitlementVersion = testVersion(t, 1, "foreign-entitlement")
		},
		"budget id": func(candidate *Reservation) {
			candidate.BudgetID = testID(t, "budget", testStart.Add(time.Second))
		},
		"budget version": func(candidate *Reservation) {
			candidate.BudgetVersion = testVersion(t, 1, "foreign-budget")
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newFixture(t, 2)
			snapshot, candidate := candidateFor(t, fixture, 1)
			mutate(&candidate)
			var err error
			candidate, err = versionReservation(candidate, 1)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := fixture.repository.Reserve(context.Background(), snapshot, candidate, fixture.clock.Now()); !faults.IsReason(err, "reservation_policy_mismatch") {
				t.Fatalf("expected policy binding rejection, got %v", err)
			}
		})
	}
}

func TestRepositoryRejectsReservationIDCollisionWithoutMutation(t *testing.T) {
	fixture := newFixture(t, 2)
	snapshot, first := candidateFor(t, fixture, 1)
	stored, replayed, err := fixture.repository.Reserve(context.Background(), snapshot, first, fixture.clock.Now())
	if err != nil || replayed {
		t.Fatalf("initial reserve failed: reservation=%+v replayed=%t error=%v", stored, replayed, err)
	}
	collision := first.clone()
	collision.Idempotency = request(2).Idempotency
	collision.RequestDigest = request(2).RequestDigest
	collision, err = versionReservation(collision, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.repository.Reserve(context.Background(), snapshot, collision, fixture.clock.Now()); !faults.IsReason(err, "reservation_id_conflict") {
		t.Fatalf("expected reservation ID collision, got %v", err)
	}
	after, err := fixture.repository.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.RequestDigest.Equal(first.RequestDigest) || after.Idempotency.Key.String() != first.Idempotency.Key.String() {
		t.Fatal("reservation collision mutated the original record")
	}
	if _, err := fixture.service.Admit(context.Background(), request(2)); err != nil {
		t.Fatalf("collision consumed ledger capacity or idempotency state: %v", err)
	}
}

func TestRepositoryRejectsForgedInitialStateTimeAndVersion(t *testing.T) {
	for name, mutate := range map[string]func(*Reservation){
		"terminal state": func(candidate *Reservation) {
			candidate.State = ReservationCommitted
			candidate.Actual = Quota{UnitRequests: 1}
			candidate.FinalizedAt = candidate.CreatedAt.Add(time.Second)
		},
		"future creation": func(candidate *Reservation) {
			candidate.CreatedAt = candidate.CreatedAt.Add(time.Second)
		},
		"past creation": func(candidate *Reservation) {
			candidate.CreatedAt = candidate.CreatedAt.Add(-time.Second)
		},
		"expiry beyond policy": func(candidate *Reservation) {
			candidate.RequestedTTL = 2 * time.Hour
			candidate.ExpiresAt = candidate.CreatedAt.Add(2 * time.Hour)
		},
		"unbound version": func(candidate *Reservation) {
			candidate.Version = testVersion(t, 1, "forged-reservation")
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newFixture(t, 2)
			snapshot, candidate := candidateFor(t, fixture, 1)
			mutate(&candidate)
			if name != "unbound version" {
				var err error
				candidate, err = versionReservation(candidate, 1)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := fixture.repository.Reserve(context.Background(), snapshot, candidate, fixture.clock.Now()); err == nil {
				t.Fatal("forged reservation was accepted")
			}
		})
	}
}

func TestAdmissionRejectsNonUnitRequestCount(t *testing.T) {
	for _, amount := range []uint64{0, 2} {
		fixture := newFixture(t, 2)
		candidate := request(1)
		candidate.Requested[UnitRequests] = amount
		if _, err := fixture.service.Admit(context.Background(), candidate); !faults.IsReason(err, "request_count_invalid") {
			t.Fatalf("request count %d: expected rejection, got %v", amount, err)
		}
	}
}

func TestCommitRequiresOneActualRequestAndRetainsCapacityOnRejection(t *testing.T) {
	fixture := newFixture(t, 1)
	decision, err := fixture.service.Admit(context.Background(), request(1))
	if err != nil {
		t.Fatal(err)
	}
	dispatched, err := fixture.service.Dispatch(context.Background(), decision.Reservation.ID, decision.Reservation.Version, decision.Reservation.RequestDigest, "service-account")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Commit(context.Background(), decision.Reservation.ID,
		dispatched.Reservation.Version, decision.Reservation.RequestDigest, "service-account",
		Quota{UnitInputTokens: 80}); !faults.IsReason(err, "actual_request_count_invalid") {
		t.Fatalf("expected zero-request commit rejection, got %v", err)
	}
	if _, err := fixture.service.Admit(context.Background(), request(2)); !faults.IsReason(err, "budget_exhausted") {
		t.Fatalf("rejected commit released request capacity: %v", err)
	}
}

func TestTypedNilDependenciesFailClosed(t *testing.T) {
	var repository *MemoryRepository
	service := Service{Repository: repository}
	if _, err := service.Admit(context.Background(), request(1)); !faults.IsReason(err, "repository_unavailable") {
		t.Fatalf("typed-nil repository on Admit was not rejected: %v", err)
	}
	if _, err := service.Commit(context.Background(), identifiers.ID{}, resourceversion.Version{}, identifiers.Digest{}, "service-account", Quota{UnitRequests: 1}); !faults.IsReason(err, "repository_unavailable") {
		t.Fatalf("typed-nil repository on Commit was not rejected: %v", err)
	}
	if _, err := service.Release(context.Background(), identifiers.ID{}, resourceversion.Version{}, identifiers.Digest{}, "service-account"); !faults.IsReason(err, "repository_unavailable") {
		t.Fatalf("typed-nil repository on Release was not rejected: %v", err)
	}
	fixture := newFixture(t, 1)
	var typedClock *clock.FakeClock
	fixture.service.Clock = typedClock
	if _, err := fixture.service.Admit(context.Background(), request(1)); !faults.IsReason(err, "clock_unavailable") {
		t.Fatalf("typed-nil clock on Admit was not rejected: %v", err)
	}
	if _, err := fixture.service.Commit(context.Background(), identifiers.ID{}, resourceversion.Version{}, identifiers.Digest{}, "service-account", Quota{UnitRequests: 1}); !faults.IsReason(err, "clock_unavailable") {
		t.Fatalf("typed-nil clock on Commit was not rejected: %v", err)
	}
	if _, err := fixture.service.Release(context.Background(), identifiers.ID{}, resourceversion.Version{}, identifiers.Digest{}, "service-account"); !faults.IsReason(err, "clock_unavailable") {
		t.Fatalf("typed-nil clock on Release was not rejected: %v", err)
	}
}

func TestReservationVersionSealAndStateUsageAreValidated(t *testing.T) {
	fixture := newFixture(t, 2)
	decision, err := fixture.service.Admit(context.Background(), request(1))
	if err != nil {
		t.Fatal(err)
	}
	tampered := decision.Reservation.clone()
	tampered.Workspace = "other-workspace"
	if err := tampered.Validate(); !faults.IsReason(err, "reservation_version_digest_mismatch") {
		t.Fatalf("reserved content tamper was not rejected: %v", err)
	}
	withActual := decision.Reservation.clone()
	withActual.Actual = Quota{UnitRequests: 1}
	if err := withActual.Validate(); !faults.IsReason(err, "reservation_reserved_lifecycle_invalid") {
		t.Fatalf("reserved actual usage was not rejected: %v", err)
	}
	dispatched, err := decision.Reservation.Dispatch(fixture.clock.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	committed, err := dispatched.Commit(Quota{UnitRequests: 1, UnitInputTokens: 80}, fixture.clock.Now().Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	committed.Actual[UnitInputTokens]++
	if err := committed.Validate(); !faults.IsReason(err, "reservation_version_digest_mismatch") {
		t.Fatalf("terminal content tamper was not rejected: %v", err)
	}
}

func TestRepeatedLateUndispatchedTransitionReturnsStableExpiration(t *testing.T) {
	for _, operation := range []string{"dispatch", "release"} {
		t.Run(operation, func(t *testing.T) {
			fixture := newFixture(t, 1)
			decision, err := fixture.service.Admit(context.Background(), request(1))
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.clock.Advance(decision.Reservation.RequestedTTL); err != nil {
				t.Fatal(err)
			}
			finalize := func() error {
				if operation == "dispatch" {
					_, err := fixture.service.Dispatch(context.Background(), decision.Reservation.ID,
						decision.Reservation.Version, decision.Reservation.RequestDigest, "service-account")
					return err
				}
				_, err := fixture.service.Release(context.Background(), decision.Reservation.ID,
					decision.Reservation.Version, decision.Reservation.RequestDigest, "service-account")
				return err
			}
			for attempt := 0; attempt < 2; attempt++ {
				if err := finalize(); !faults.IsReason(err, "reservation_expired") {
					t.Fatalf("attempt %d returned unstable expiration: %v", attempt+1, err)
				}
			}
		})
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
	dispatched, err := reservation.Dispatch(now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if dispatched.State != ReservationDispatched || dispatched.Version.Generation() != reservation.Version.Generation()+1 {
		t.Fatalf("dispatch did not create the next version: %+v", dispatched)
	}
	committed, err := dispatched.Commit(Quota{UnitRequests: 1}, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if committed.State != ReservationCommitted || committed.Version.Generation() != dispatched.Version.Generation()+1 {
		t.Fatalf("commit did not create the next terminal version: %+v", committed)
	}
	if _, err := committed.Release(now.Add(3 * time.Second)); !faults.IsReason(err, "reservation_already_dispatched") {
		t.Fatalf("expected terminal transition rejection, got %v", err)
	}
	if _, err := reservation.Dispatch(reservation.ExpiresAt); !faults.IsReason(err, "reservation_expired") {
		t.Fatalf("expected exact-boundary dispatch rejection, got %v", err)
	}
	if _, err := reservation.Release(reservation.ExpiresAt); !faults.IsReason(err, "reservation_expired") {
		t.Fatalf("expected exact-boundary release rejection, got %v", err)
	}
	reservedWithFinalization := reservation
	reservedWithFinalization.FinalizedAt = now
	if err := reservedWithFinalization.Validate(); !faults.IsReason(err, "reservation_reserved_lifecycle_invalid") {
		t.Fatalf("expected reserved finalization rejection, got %v", err)
	}
	expiredBeforeDeadline := reservation
	expiredBeforeDeadline.State = ReservationExpired
	expiredBeforeDeadline.FinalizedAt = reservation.ExpiresAt.Add(-time.Nanosecond)
	expiredBeforeDeadline, err = versionReservation(expiredBeforeDeadline, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := expiredBeforeDeadline.Validate(); !faults.IsReason(err, "reservation_expiration_time_invalid") {
		t.Fatalf("expected premature expiration rejection, got %v", err)
	}
}

func TestPolicyVersionsSealCanonicalContent(t *testing.T) {
	fixture := newFixture(t, 2)
	snapshot, err := fixture.repository.Snapshot(context.Background(), "service-account", "research-team")
	if err != nil {
		t.Fatal(err)
	}
	changed := snapshot.Entitlement
	changed.PolicyEpoch++
	if err := changed.Validate(); !faults.IsReason(err, "entitlement_version_digest_mismatch") {
		t.Fatalf("expected entitlement seal failure, got %v", err)
	}
	changedBudget := snapshot.Budget
	changedBudget.Limit[UnitRequests]++
	if err := changedBudget.Validate(); !faults.IsReason(err, "budget_version_digest_mismatch") {
		t.Fatalf("expected budget seal failure, got %v", err)
	}

	unsorted := snapshot.Entitlement
	unsorted.Routes = []GatewayRoute{
		{Endpoint: "secondary", Provider: "vertex", Model: "gemini-flash"},
		fixture.route,
	}
	sealed, err := unsorted.Seal(2)
	if err != nil {
		t.Fatal(err)
	}
	reversed := unsorted
	reversed.Routes[0], reversed.Routes[1] = reversed.Routes[1], reversed.Routes[0]
	resealed, err := reversed.Seal(2)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.Version.String() != resealed.Version.String() {
		t.Fatalf("route order changed canonical policy identity: %s != %s", sealed.Version, resealed.Version)
	}
}
