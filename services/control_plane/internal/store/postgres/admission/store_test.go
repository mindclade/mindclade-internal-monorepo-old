// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admissionpostgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/audit"
	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/coordination/outbox"
	"go.mindclade.dev/libs/go/coordination/outbox/memory"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/requestmeta"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/libs/go/storage/sql/sqltest"
	"go.mindclade.dev/libs/go/storage/sql/transaction"
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
	for _, required := range []string{
		"UNIQUE (idempotency_scope, idempotency_key)",
		"budget_id, state, expires_at",
		"document->>'request_digest' IS NOT DISTINCT FROM request_digest",
		"document#>>'{reserved,requests}'",
		"reserved_requests = 1",
		"NULLIF(document->>'finalized_at', '0001-01-01T00:00:00Z')::TIMESTAMPTZ IS NOT DISTINCT FROM finalized_at",
		"split_part(resource_version, ':', 2)::NUMERIC IS NOT DISTINCT FROM resource_generation",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("DDL lacks %q", required)
		}
	}
	const generationParity = "split_part(resource_version, ':', 2)::NUMERIC IS NOT DISTINCT FROM resource_generation"
	if count := strings.Count(joined, generationParity); count != 3 {
		t.Fatalf("DDL generation parity checks=%d, want 3", count)
	}
	if strings.Contains(joined, "CREATE TABLE IF NOT EXISTS") {
		t.Fatal("admission migrations silently accept pre-existing table shapes")
	}
	if _, err := DDL("safe; DROP TABLE users", DefaultBudgetTable, DefaultReservationTable); err == nil {
		t.Fatal("unsafe table identifier was accepted")
	}
}

func TestObservabilityDDLIsAdditiveBoundedAndRejectsUnsafeIdentifiers(t *testing.T) {
	statement, err := ObservabilityDDL(DefaultReservationTable, "mindclade_audit_events", "mindclade_outbox", "mindclade_work_items")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"(expires_at,reservation_id) WHERE state='reserved'",
		"(occurred_at DESC,event_id DESC) WHERE target_type='gateway_reservation'",
		"(created_at DESC,message_id DESC) WHERE topic='control.admission.reservation.v1'",
		"((headers->>'audit-event-id')) WHERE topic='control.admission.reservation.v1' AND headers ? 'audit-event-id'",
		"(queue,completed_at DESC,item_id DESC) WHERE state='completed'",
	} {
		if !strings.Contains(statement, required) {
			t.Fatalf("observability DDL lacks %q: %s", required, statement)
		}
	}
	if strings.Contains(statement, "CREATE TABLE") || strings.Contains(statement, "ALTER TABLE") || strings.Contains(statement, "DROP ") {
		t.Fatalf("observability migration is not additive index-only DDL: %s", statement)
	}
	if _, err := ObservabilityDDL(DefaultReservationTable, "bad-table", "mindclade_outbox", "mindclade_work_items"); err == nil {
		t.Fatal("unsafe observability table identifier was accepted")
	}
	maximumQualifiedTable := strings.Repeat("s", 63) + "." + strings.Repeat("t", 63)
	if _, err := ObservabilityDDL(maximumQualifiedTable, maximumQualifiedTable, maximumQualifiedTable, maximumQualifiedTable); err != nil {
		t.Fatalf("maximum valid qualified identifiers were rejected: %v", err)
	}
	for _, suffix := range []string{"expiration_observability_idx", "admission_observability_idx", "admission_recent_idx", "admission_audit_event_idx", "completed_observability_idx"} {
		if generated := indexName(maximumQualifiedTable, suffix); len(generated) > maximumPostgresIdentifierBytes {
			t.Fatalf("generated index %q is %d bytes, maximum is %d", generated, len(generated), maximumPostgresIdentifierBytes)
		}
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

func TestReadinessRequiresEveryAdmissionTableShape(t *testing.T) {
	fixture := newDomainFixture(t)
	queryIndex := 0
	state := &sqltest.State{Query: func(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
		queryIndex++
		if !strings.Contains(query, "LIMIT 0") {
			t.Fatalf("readiness query is not metadata-only: %s", query)
		}
		if queryIndex == 2 {
			return nil, errors.New("budget table missing")
		}
		return sqltest.NewRows([]string{"shape"}), nil
	}}
	store, _ := newTestStore(t, state, fixture.clock, audit.NopRecorder{})
	if err := store.Readiness(context.Background()); err == nil {
		t.Fatal("readiness accepted a missing admission table")
	}
	if queryIndex != 2 {
		t.Fatalf("readiness queries=%d", queryIndex)
	}

	queryIndex = 0
	state.Query = func(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
		queryIndex++
		return sqltest.NewRows([]string{"shape"}), nil
	}
	if err := store.Readiness(context.Background()); err != nil {
		t.Fatal(err)
	}
	if queryIndex != 3 {
		t.Fatalf("readiness queries=%d", queryIndex)
	}
	component := store.Component("admission-schema")
	if component.Readiness == nil || component.Start != nil || component.Run != nil {
		t.Fatalf("readiness component=%#v", component)
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
	var recorded audit.Event
	recorder := audit.RecorderFunc(func(_ context.Context, event audit.Event) error {
		recorded = event
		return nil
	})
	store, messages := newTestStore(t, state, fixture.clock, recorder)
	requestID, err := requestmeta.NewRequestIDAt(testNow)
	if err != nil {
		t.Fatal(err)
	}
	metadata := requestmeta.Metadata{
		RequestID: requestID,
		Operation: requestmeta.MustParseOperation("ai_gateway.reservations.create"),
	}
	ctx, err := requestmeta.WithMetadata(context.Background(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := auth.NewPrincipal(auth.PrincipalKindService, "service-account", auth.WithIssuer("test-issuer"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = auth.WithPrincipal(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	reservation, replayed, err := store.Reserve(ctx, fixture.snapshot, fixture.reservation, testNow)
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
	if recorded.Actor().Subject() != principal.Subject() || recorded.Actor().Issuer() != principal.Issuer() {
		t.Fatalf("audit actor=%s issuer=%s", recorded.Actor().Subject(), recorded.Actor().Issuer())
	}
	auditMetadata, ok := recorded.RequestMetadata()
	if !ok || auditMetadata.RequestID != requestID || claims[0].Message().Request().RequestID != requestID {
		t.Fatalf("request lineage missing from audit/outbox: audit=%#v outbox=%#v", auditMetadata, claims[0].Message().Request())
	}
	headers := claims[0].Message().Headers()
	wantHeaders := map[string]string{
		LineageSchemaVersionHeader:   fmt.Sprint(ReservationEventSchemaVersion),
		LineageAuditEventIDHeader:    recorded.ID().String(),
		LineageAuditActionHeader:     recorded.Action().String(),
		LineageTargetTypeHeader:      recorded.Target().Type(),
		LineageTargetIDHeader:        reservation.ID.String(),
		LineageResourceVersionHeader: reservation.Version.String(),
	}
	if len(headers) != len(wantHeaders) {
		t.Fatalf("reservation lineage headers=%v, want exactly %v", headers, wantHeaders)
	}
	for key, want := range wantHeaders {
		if headers[key] != want {
			t.Fatalf("reservation lineage header %q=%q, want %q", key, headers[key], want)
		}
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

func TestReserveRetriesConcurrentIdempotencyWinner(t *testing.T) {
	fixture := newDomainFixture(t)
	entitlementDocument, _ := json.Marshal(fixture.snapshot.Entitlement)
	budgetDocument, _ := json.Marshal(fixture.snapshot.Budget)
	reservationDocument, _ := json.Marshal(fixture.reservation)
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
			case 5:
				return sqltest.NewRows([]string{"document"}, []driver.Value{reservationDocument}), nil
			default:
				return nil, fmt.Errorf("unexpected query %d", queryIndex)
			}
		},
		Exec: func(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
			if !strings.Contains(query, "ON CONFLICT (idempotency_scope,idempotency_key) DO NOTHING") {
				t.Fatalf("idempotency insert is not conflict-safe: %s", query)
			}
			return driver.RowsAffected(0), nil
		},
	}
	store, _ := newTestStore(t, state, fixture.clock, audit.NopRecorder{})
	policy, err := retry.NewPolicy(retry.WithMaxAttempts(2), retry.WithoutJitter())
	if err != nil {
		t.Fatal(err)
	}
	store.retries, err = retry.NewExecutor(policy, retry.WithClock(fixture.clock))
	if err != nil {
		t.Fatal(err)
	}
	reservation, replayed, err := store.Reserve(context.Background(), fixture.snapshot, fixture.reservation, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed || reservation.ID.String() != fixture.reservation.ID.String() {
		t.Fatalf("reservation=%s replayed=%t", reservation.ID, replayed)
	}
	if state.Begins.Load() != 2 || state.Rollbacks.Load() != 1 || state.Commits.Load() != 1 {
		t.Fatalf("begins=%d rollbacks=%d commits=%d", state.Begins.Load(), state.Rollbacks.Load(), state.Commits.Load())
	}
}

func TestReserveUsesFreshClockAfterPolicyLocks(t *testing.T) {
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
				if err := fixture.clock.Set(fixture.reservation.ExpiresAt); err != nil {
					t.Fatal(err)
				}
				return sqltest.NewRows([]string{"document"}, []driver.Value{budgetDocument}), nil
			default:
				return nil, fmt.Errorf("stale decision reached query %d", queryIndex)
			}
		},
		Exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
			t.Fatal("elapsed reservation reached persistence")
			return nil, nil
		},
	}
	store, _ := newTestStore(t, state, fixture.clock, audit.NopRecorder{})
	_, _, err := store.Reserve(context.Background(), fixture.snapshot, fixture.reservation, testNow)
	if !faults.IsReason(err, "reservation_window_elapsed") {
		t.Fatalf("stale-time reason=%q error=%v", faults.ReasonOf(err), err)
	}
	if state.Commits.Load() != 0 || state.Rollbacks.Load() != 1 {
		t.Fatalf("commits=%d rollbacks=%d", state.Commits.Load(), state.Rollbacks.Load())
	}
}

func TestReserveReplaysAcrossServerPolicyRotation(t *testing.T) {
	fixture := newDomainFixture(t)
	rotatedEntitlement, err := fixture.snapshot.Entitlement.Seal(2)
	if err != nil {
		t.Fatal(err)
	}
	rotatedBudget, err := fixture.snapshot.Budget.Seal(2)
	if err != nil {
		t.Fatal(err)
	}
	repository := admission.NewMemoryRepository(10)
	if err := repository.PutEntitlement(context.Background(), rotatedEntitlement); err != nil {
		t.Fatal(err)
	}
	if err := repository.PutBudget(context.Background(), rotatedBudget); err != nil {
		t.Fatal(err)
	}
	decision, err := (admission.Service{Repository: repository, Clock: fixture.clock}).Admit(context.Background(), admission.AdmitRequest{
		Idempotency: fixture.reservation.Idempotency, RequestDigest: fixture.reservation.RequestDigest,
		Subject: fixture.reservation.Subject, Workspace: fixture.reservation.Workspace,
		Route: fixture.reservation.Route, PolicyEpoch: fixture.reservation.PolicyEpoch,
		Requested: fixture.reservation.Reserved.Clone(), TTL: fixture.reservation.RequestedTTL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Reservation.EntitlementVersion.String() == fixture.reservation.EntitlementVersion.String() ||
		decision.Reservation.BudgetVersion.String() == fixture.reservation.BudgetVersion.String() {
		t.Fatal("rotated fixture did not change server-owned policy versions")
	}
	document, _ := json.Marshal(fixture.reservation)
	state := &sqltest.State{Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return sqltest.NewRows([]string{"document"}, []driver.Value{document}), nil
	}}
	store, _ := newTestStore(t, state, fixture.clock, audit.NopRecorder{})
	replayed, wasReplay, err := store.Reserve(context.Background(),
		admission.PolicySnapshot{Entitlement: rotatedEntitlement, Budget: rotatedBudget}, decision.Reservation, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if !wasReplay || replayed.Version.String() != fixture.reservation.Version.String() {
		t.Fatalf("replayed=%t version=%s", wasReplay, replayed.Version)
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
	if err := fixture.clock.Set(fixture.reservation.ExpiresAt); err != nil {
		t.Fatal(err)
	}
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

func TestMutationRejectsUnqualifiedOuterTransaction(t *testing.T) {
	fixture := newDomainFixture(t)
	state := &sqltest.State{}
	store, _ := newTestStore(t, state, fixture.clock, audit.NopRecorder{})
	_, err := transaction.Run(context.Background(), store.db,
		transaction.Options{Isolation: 0},
		func(ctx context.Context, _ *sql.Tx) (struct{}, error) {
			return struct{}{}, store.PutBudget(ctx, fixture.snapshot.Budget)
		})
	if !faults.IsReason(err, "admission_nested_transaction_unsupported") {
		t.Fatalf("nested transaction reason=%q error=%v", faults.ReasonOf(err), err)
	}
	if state.Executions.Load() != 0 || state.Commits.Load() != 0 || state.Rollbacks.Load() != 1 {
		t.Fatalf("executions=%d commits=%d rollbacks=%d", state.Executions.Load(), state.Commits.Load(), state.Rollbacks.Load())
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

func TestFinalizationUsesStoreClockInsteadOfCallerTimestamp(t *testing.T) {
	for _, test := range []struct {
		name     string
		finalize func(*Store, domainFixture, time.Time) (admission.Reservation, bool, error)
	}{
		{
			name: "commit",
			finalize: func(store *Store, fixture domainFixture, forged time.Time) (admission.Reservation, bool, error) {
				return store.Commit(
					context.Background(), fixture.reservation.ID, fixture.reservation.Version,
					fixture.reservation.RequestDigest, fixture.reservation.Subject,
					admission.Quota{
						admission.UnitRequests: 1, admission.UnitInputTokens: 90,
						admission.UnitOutputTokens: 40, admission.UnitCostMicros: 450,
					}, forged,
				)
			},
		},
		{
			name: "release",
			finalize: func(store *Store, fixture domainFixture, forged time.Time) (admission.Reservation, bool, error) {
				return store.Release(
					context.Background(), fixture.reservation.ID, fixture.reservation.Version,
					fixture.reservation.RequestDigest, fixture.reservation.Subject, forged,
				)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDomainFixture(t)
			decisionNow := testNow.Add(10 * time.Second)
			if err := fixture.clock.Set(decisionNow); err != nil {
				t.Fatal(err)
			}
			document, _ := json.Marshal(fixture.reservation)
			var persistedFinalization time.Time
			state := &sqltest.State{
				Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
					return sqltest.NewRows([]string{"document"}, []driver.Value{document}), nil
				},
				Exec: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
					if !strings.Contains(query, "UPDATE "+DefaultReservationTable) {
						t.Fatalf("unexpected finalization mutation: %s", query)
					}
					var ok bool
					persistedFinalization, ok = arguments[6].Value.(time.Time)
					if !ok {
						t.Fatalf("finalized_at argument type = %T", arguments[6].Value)
					}
					return driver.RowsAffected(1), nil
				},
			}
			store, _ := newTestStore(t, state, fixture.clock, audit.NopRecorder{})
			forged := fixture.reservation.ExpiresAt.Add(time.Hour)
			result, replayed, err := test.finalize(store, fixture, forged)
			if err != nil {
				t.Fatalf("finalization trusted forged expired timestamp: %v", err)
			}
			if replayed || !result.FinalizedAt.Equal(decisionNow) || !persistedFinalization.Equal(decisionNow) {
				t.Fatalf(
					"replayed=%t result finalized_at=%s persisted finalized_at=%s, want %s",
					replayed, result.FinalizedAt, persistedFinalization, decisionNow,
				)
			}
		})
	}
}

func TestFinalizationSamplesStoreClockAfterRowLock(t *testing.T) {
	for _, test := range []struct {
		name     string
		finalize func(*Store, domainFixture) (admission.Reservation, bool, error)
	}{
		{
			name: "commit",
			finalize: func(store *Store, fixture domainFixture) (admission.Reservation, bool, error) {
				return store.Commit(
					context.Background(), fixture.reservation.ID, fixture.reservation.Version,
					fixture.reservation.RequestDigest, fixture.reservation.Subject,
					admission.Quota{
						admission.UnitRequests: 1, admission.UnitInputTokens: 90,
						admission.UnitOutputTokens: 40, admission.UnitCostMicros: 450,
					}, testNow,
				)
			},
		},
		{
			name: "release",
			finalize: func(store *Store, fixture domainFixture) (admission.Reservation, bool, error) {
				return store.Release(
					context.Background(), fixture.reservation.ID, fixture.reservation.Version,
					fixture.reservation.RequestDigest, fixture.reservation.Subject, testNow,
				)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDomainFixture(t)
			document, _ := json.Marshal(fixture.reservation)
			var persistedState string
			var persistedFinalization time.Time
			state := &sqltest.State{
				Query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
					if err := fixture.clock.Set(fixture.reservation.ExpiresAt); err != nil {
						t.Fatal(err)
					}
					return sqltest.NewRows([]string{"document"}, []driver.Value{document}), nil
				},
				Exec: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
					if !strings.Contains(query, "UPDATE "+DefaultReservationTable) {
						t.Fatalf("unexpected finalization mutation: %s", query)
					}
					persistedState, _ = arguments[1].Value.(string)
					persistedFinalization, _ = arguments[6].Value.(time.Time)
					return driver.RowsAffected(1), nil
				},
			}
			store, _ := newTestStore(t, state, fixture.clock, audit.NopRecorder{})
			_, _, err := test.finalize(store, fixture)
			if !faults.IsReason(err, "reservation_expired") {
				t.Fatalf("post-lock expiry reason=%q error=%v", faults.ReasonOf(err), err)
			}
			if persistedState != string(admission.ReservationExpired) ||
				!persistedFinalization.Equal(fixture.reservation.ExpiresAt) {
				t.Fatalf(
					"persisted state=%q finalized_at=%s, want expired at %s",
					persistedState, persistedFinalization, fixture.reservation.ExpiresAt,
				)
			}
			if state.Commits.Load() != 1 || state.Rollbacks.Load() != 0 {
				t.Fatalf("commits=%d rollbacks=%d", state.Commits.Load(), state.Rollbacks.Load())
			}
		})
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
