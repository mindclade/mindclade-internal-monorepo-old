// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admissionpostgres

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/audit"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/coordination/outbox"
	"go.mindclade.dev/libs/go/coordination/outbox/memory"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/libs/go/storage/sql/sqltest"
)

var testNow = time.Date(2026, time.August, 21, 8, 0, 0, 0, time.UTC)

type domainFixture struct {
	clock       *clock.FakeClock
	snapshot    admission.PolicySnapshot
	reservation admission.Reservation
}

func newDomainFixture(t *testing.T) domainFixture {
	t.Helper()
	fake := clock.NewFake(testNow)
	route := admission.GatewayRoute{Endpoint: "chat-primary", Provider: "vertex", Model: "gemini-pro"}
	entitlement := admission.Entitlement{
		ID: idAt(t, "entitlement", testNow), Subject: "service-account", Workspace: "research-team",
		PolicyEpoch: 7, Routes: []admission.GatewayRoute{route},
		MaximumRequest: admission.Quota{admission.UnitRequests: 1, admission.UnitInputTokens: 1000, admission.UnitOutputTokens: 500, admission.UnitCostMicros: 5000},
		NotBefore:      testNow.Add(-time.Minute), ExpiresAt: testNow.Add(time.Hour),
	}
	var err error
	entitlement, err = entitlement.Seal(1)
	if err != nil {
		t.Fatal(err)
	}
	budget := admission.Budget{
		ID: idAt(t, "budget", testNow.Add(time.Millisecond)), Workspace: "research-team",
		Limit:    admission.Quota{admission.UnitRequests: 2, admission.UnitInputTokens: 2000, admission.UnitOutputTokens: 1000, admission.UnitCostMicros: 10000},
		StartsAt: testNow.Add(-time.Minute), ExpiresAt: testNow.Add(time.Hour),
	}
	budget, err = budget.Seal(1)
	if err != nil {
		t.Fatal(err)
	}
	repository := admission.NewMemoryRepository(10)
	if err := repository.PutEntitlement(context.Background(), entitlement); err != nil {
		t.Fatal(err)
	}
	if err := repository.PutBudget(context.Background(), budget); err != nil {
		t.Fatal(err)
	}
	request := admission.AdmitRequest{
		Idempotency: idempotency.Identity{
			Scope: idempotency.MustParseScope("research-team/mlflow-gateway/service-account"),
			Key:   idempotency.MustParseKey("request-0001"),
		},
		RequestDigest: identifiers.SHA256String("payload"), Subject: "service-account", Workspace: "research-team",
		Route: route, PolicyEpoch: 7,
		Requested: admission.Quota{admission.UnitRequests: 1, admission.UnitInputTokens: 100, admission.UnitOutputTokens: 50, admission.UnitCostMicros: 500},
		TTL:       30 * time.Second,
	}
	decision, err := (admission.Service{Repository: repository, Clock: fake}).Admit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return domainFixture{clock: fake, snapshot: admission.PolicySnapshot{Entitlement: entitlement, Budget: budget}, reservation: decision.Reservation}
}

func idAt(t *testing.T, kind string, at time.Time) identifiers.ID {
	t.Helper()
	id, err := identifiers.NewIDAt(identifiers.MustParseKind(kind), at)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func newTestStore(t *testing.T, state *sqltest.State, fake *clock.FakeClock, recorder audit.Recorder) (*Store, *memory.Store) {
	t.Helper()
	database, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	messages, err := memory.New(memory.WithClock(fake))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := retry.NewPolicy(retry.WithMaxAttempts(1))
	if err != nil {
		t.Fatal(err)
	}
	retries, err := retry.NewExecutor(policy, retry.WithClock(fake))
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(database, recorder, messages, WithClock(fake), WithRetry(retries))
	if err != nil {
		t.Fatal(err)
	}
	return store, messages
}

func TestDDLIsCompleteAndRejectsUnsafeIdentifiers(t *testing.T) {
	statements, err := DDL(DefaultEntitlementTable, DefaultBudgetTable, DefaultReservationTable)
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 3 {
		t.Fatalf("DDL statements=%d", len(statements))
	}
	joined := strings.Join(statements, "\n")
	for _, required := range []string{"UNIQUE (idempotency_scope, idempotency_key)", "FOR UPDATE", "budget_id, state, expires_at"} {
		if required == "FOR UPDATE" {
			continue
		}
		if !strings.Contains(joined, required) {
			t.Fatalf("DDL lacks %q", required)
		}
	}
	if _, err := DDL("safe; DROP TABLE users", DefaultBudgetTable, DefaultReservationTable); err == nil {
		t.Fatal("unsafe table identifier was accepted")
	}
}

func TestSnapshotRevalidatesStoredPolicySeals(t *testing.T) {
	fixture := newDomainFixture(t)
	entitlementDocument, _ := json.Marshal(fixture.snapshot.Entitlement)
	budgetDocument, _ := json.Marshal(fixture.snapshot.Budget)
	state := &sqltest.State{Query: func(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
		if strings.Contains(query, DefaultEntitlementTable) {
			return sqltest.NewRows([]string{"document"}, []driver.Value{entitlementDocument}), nil
		}
		return sqltest.NewRows([]string{"document"}, []driver.Value{budgetDocument}), nil
	}}
	store, _ := newTestStore(t, state, fixture.clock, audit.NopRecorder{})
	snapshot, err := store.Snapshot(context.Background(), "service-account", "research-team")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Entitlement.Version.String() != fixture.snapshot.Entitlement.Version.String() || snapshot.Budget.Version.String() != fixture.snapshot.Budget.Version.String() {
		t.Fatal("snapshot changed policy versions")
	}

	tampered := fixture.snapshot.Entitlement
	tampered.PolicyEpoch++
	tamperedDocument, _ := json.Marshal(tampered)
	state.Query = func(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
		if strings.Contains(query, DefaultEntitlementTable) {
			return sqltest.NewRows([]string{"document"}, []driver.Value{tamperedDocument}), nil
		}
		return sqltest.NewRows([]string{"document"}, []driver.Value{budgetDocument}), nil
	}
	if _, err := store.Snapshot(context.Background(), "service-account", "research-team"); !faults.IsReason(err, "admission_document_invalid") {
		t.Fatalf("tampered policy reason=%q error=%v", faults.ReasonOf(err), err)
	}
}

func TestReserveRunsOneSerializableMutationAndEmitsOutbox(t *testing.T) {
	fixture := newDomainFixture(t)
	entitlementDocument, _ := json.Marshal(fixture.snapshot.Entitlement)
	budgetDocument, _ := json.Marshal(fixture.snapshot.Budget)
	queryIndex := 0
	state := &sqltest.State{
		Query: func(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
			queryIndex++
			switch queryIndex {
			case 1:
				return sqltest.NewRows([]string{"document"}), nil
			case 2:
				return sqltest.NewRows([]string{"document"}, []driver.Value{entitlementDocument}), nil
			case 3:
				return sqltest.NewRows([]string{"document"}, []driver.Value{budgetDocument}), nil
			case 4:
				return sqltest.NewRows([]string{"requests", "input", "output", "cost"}, []driver.Value{int64(0), int64(0), int64(0), int64(0)}), nil
			default:
				return nil, fmt.Errorf("unexpected query %d", queryIndex)
			}
		},
		Exec: func(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
			if !strings.Contains(query, "INSERT INTO "+DefaultReservationTable) {
				t.Fatalf("unexpected SQL mutation: %s", query)
			}
			return driver.RowsAffected(1), nil
		},
	}
	store, messages := newTestStore(t, state, fixture.clock, audit.NopRecorder{})
	reservation, replayed, err := store.Reserve(context.Background(), fixture.snapshot, fixture.reservation, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if replayed || reservation.ID.String() != fixture.reservation.ID.String() {
		t.Fatalf("reservation=%s replayed=%t", reservation.ID, replayed)
	}
	if state.Begins.Load() != 1 || state.Commits.Load() != 1 || state.Rollbacks.Load() != 0 {
		t.Fatalf("transaction begins=%d commits=%d rollbacks=%d", state.Begins.Load(), state.Commits.Load(), state.Rollbacks.Load())
	}
	claims, err := messages.Claim(context.Background(), outbox.ClaimRequest{
		Owner: "test-worker", Topics: []string{"control.admission.reservation.v1"},
		Limit: 1, LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].Message().Topic() != "control.admission.reservation.v1" {
		t.Fatalf("outbox claims=%d", len(claims))
	}
}

func TestReserveRollsBackWhenBudgetIsExhausted(t *testing.T) {
	fixture := newDomainFixture(t)
	entitlementDocument, _ := json.Marshal(fixture.snapshot.Entitlement)
	budgetDocument, _ := json.Marshal(fixture.snapshot.Budget)
	queryIndex := 0
	state := &sqltest.State{
		Query: func(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
			queryIndex++
			switch queryIndex {
			case 1:
				return sqltest.NewRows([]string{"document"}), nil
			case 2:
				return sqltest.NewRows([]string{"document"}, []driver.Value{entitlementDocument}), nil
			case 3:
				return sqltest.NewRows([]string{"document"}, []driver.Value{budgetDocument}), nil
			default:
				return sqltest.NewRows([]string{"requests", "input", "output", "cost"}, []driver.Value{int64(2), int64(0), int64(0), int64(0)}), nil
			}
		},
		Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
			t.Fatal("exhausted reservation reached an INSERT")
			return nil, nil
		},
	}
	store, _ := newTestStore(t, state, fixture.clock, audit.NopRecorder{})
	_, _, err := store.Reserve(context.Background(), fixture.snapshot, fixture.reservation, testNow)
	if !faults.IsReason(err, "budget_exhausted") {
		t.Fatalf("expected budget exhaustion, got %v", err)
	}
	if state.Commits.Load() != 0 || state.Rollbacks.Load() != 1 {
		t.Fatalf("commits=%d rollbacks=%d", state.Commits.Load(), state.Rollbacks.Load())
	}
}

func TestExpiredIdempotencyReplayCommitsExpirationAndFails(t *testing.T) {
	fixture := newDomainFixture(t)
	document, _ := json.Marshal(fixture.reservation)
	state := &sqltest.State{
		Query: func(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
			if !strings.Contains(query, "FOR UPDATE") {
				t.Fatalf("idempotency replay was not locked: %s", query)
			}
			return sqltest.NewRows([]string{"document"}, []driver.Value{document}), nil
		},
		Exec: func(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
			if !strings.Contains(query, "UPDATE "+DefaultReservationTable) {
				t.Fatalf("unexpected expiration mutation: %s", query)
			}
			return driver.RowsAffected(1), nil
		},
	}
	store, messages := newTestStore(t, state, fixture.clock, audit.NopRecorder{})
	_, _, err := store.Reserve(context.Background(), fixture.snapshot, fixture.reservation, fixture.reservation.ExpiresAt)
	if !faults.IsReason(err, "reservation_expired") {
		t.Fatalf("expected expired replay, got %v", err)
	}
	if state.Commits.Load() != 1 || state.Rollbacks.Load() != 0 {
		t.Fatalf("expiration commits=%d rollbacks=%d", state.Commits.Load(), state.Rollbacks.Load())
	}
	claims, err := messages.Claim(context.Background(), outbox.ClaimRequest{
		Owner: "test-worker", Topics: []string{"control.admission.reservation.v1"}, Limit: 1, LeaseDuration: time.Minute,
	})
	if err != nil || len(claims) != 1 {
		t.Fatalf("expiration outbox claims=%d error=%v", len(claims), err)
	}
}

func TestFinalizationRejectsForeignSubjectBeforeMutation(t *testing.T) {
	fixture := newDomainFixture(t)
	document, _ := json.Marshal(fixture.reservation)
	state := &sqltest.State{
		Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			return sqltest.NewRows([]string{"document"}, []driver.Value{document}), nil
		},
		Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
			t.Fatal("foreign subject reached a reservation mutation")
			return nil, nil
		},
	}
	store, _ := newTestStore(t, state, fixture.clock, audit.NopRecorder{})
	_, _, err := store.Commit(context.Background(), fixture.reservation.ID, fixture.reservation.Version,
		fixture.reservation.RequestDigest, "other-client", admission.Quota{admission.UnitRequests: 1}, testNow)
	if !faults.IsReason(err, "reservation_subject_mismatch") {
		t.Fatalf("expected subject mismatch, got %v", err)
	}
	if state.Commits.Load() != 0 || state.Rollbacks.Load() != 1 {
		t.Fatalf("commits=%d rollbacks=%d", state.Commits.Load(), state.Rollbacks.Load())
	}
}

func TestPolicyMutationRollsBackWhenAuditFails(t *testing.T) {
	fixture := newDomainFixture(t)
	sentinel := errors.New("audit unavailable")
	state := &sqltest.State{Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		return driver.RowsAffected(1), nil
	}}
	recorder := audit.RecorderFunc(func(context.Context, audit.Event) error { return sentinel })
	store, _ := newTestStore(t, state, fixture.clock, recorder)
	err := store.PutBudget(context.Background(), fixture.snapshot.Budget)
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected audit failure, got %v", err)
	}
	if state.Commits.Load() != 0 || state.Rollbacks.Load() != 1 {
		t.Fatalf("commits=%d rollbacks=%d", state.Commits.Load(), state.Rollbacks.Load())
	}
}
