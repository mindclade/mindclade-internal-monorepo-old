// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admissionpostgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"go.mindclade.dev/control/admission"
	auditpostgres "go.mindclade.dev/libs/go/audit/postgres"
	"go.mindclade.dev/libs/go/clock"
	outboxpostgres "go.mindclade.dev/libs/go/coordination/outbox/postgres"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/identifiers"
)

const livePostgresEnvironment = "MINDCLADE_TEST_POSTGRES_DSN"

var liveAdmissionSchemaSequence atomic.Uint64

type liveAdmissionStore struct {
	store            *Store
	service          admission.Service
	clock            clock.Clock
	db               *sql.DB
	auditTable       string
	outboxTable      string
	entitlementTable string
	budgetTable      string
	reservationTable string
}

func newLiveAdmissionStore(t *testing.T) liveAdmissionStore {
	t.Helper()
	return newLiveAdmissionStoreWithClock(t, clock.RealClock{})
}

func newLiveAdmissionStoreWithClock(t *testing.T, valueClock clock.Clock) liveAdmissionStore {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(livePostgresEnvironment))
	if dsn == "" {
		t.Skipf("%s is not set; live PostgreSQL qualification is opt-in", livePostgresEnvironment)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(32)
	db.SetMaxIdleConns(16)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("connect to live PostgreSQL: %v", err)
	}

	schema := fmt.Sprintf("mc_admission_qual_%d_%d", os.Getpid(), liveAdmissionSchemaSequence.Add(1))
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, openErr := sql.Open("postgres", dsn)
		if openErr == nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, _ = cleanup.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
			cleanupCancel()
			_ = cleanup.Close()
		}
		_ = db.Close()
	})

	auditTable := schema + ".audit_events"
	outboxTable := schema + ".outbox_messages"
	entitlementTable := schema + ".gateway_entitlements"
	budgetTable := schema + ".gateway_budgets"
	reservationTable := schema + ".gateway_reservations"
	auditDDL, err := auditpostgres.DDL(auditTable)
	if err != nil {
		t.Fatal(err)
	}
	outboxDDL, err := outboxpostgres.DDL(outboxTable)
	if err != nil {
		t.Fatal(err)
	}
	admissionDDL, err := DDL(entitlementTable, budgetTable, reservationTable)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range append([]string{auditDDL, outboxDDL}, admissionDDL...) {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("apply admission DDL: %v", err)
		}
	}
	recorder, err := auditpostgres.New(db, auditpostgres.WithTable(auditTable))
	if err != nil {
		t.Fatal(err)
	}
	messages, err := outboxpostgres.New(db, outboxTable)
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(db, recorder, messages,
		WithClock(valueClock), WithTables(entitlementTable, budgetTable, reservationTable))
	if err != nil {
		t.Fatal(err)
	}
	return liveAdmissionStore{
		store: store, service: admission.Service{Repository: store, Clock: valueClock}, clock: valueClock, db: db,
		auditTable: auditTable, outboxTable: outboxTable, entitlementTable: entitlementTable,
		budgetTable: budgetTable, reservationTable: reservationTable,
	}
}

func (live liveAdmissionStore) seedPolicy(t *testing.T, limit uint64) admission.GatewayRoute {
	t.Helper()
	now := live.clock.Now().Round(0).UTC()
	route := admission.GatewayRoute{Endpoint: "chat-primary", Provider: "vertex", Model: "gemini-pro"}
	entitlement := admission.Entitlement{
		ID: idAt(t, "entitlement", now), Subject: "service-account", Workspace: "research-team",
		PolicyEpoch: 7, Routes: []admission.GatewayRoute{route},
		MaximumRequest: admission.Quota{
			admission.UnitRequests: 1, admission.UnitInputTokens: 1000,
			admission.UnitOutputTokens: 500, admission.UnitCostMicros: 5000,
		},
		NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	var err error
	entitlement, err = entitlement.Seal(1)
	if err != nil {
		t.Fatal(err)
	}
	budget := admission.Budget{
		ID: idAt(t, "budget", now.Add(time.Millisecond)), Workspace: "research-team",
		Limit: admission.Quota{
			admission.UnitRequests: limit, admission.UnitInputTokens: limit * 1000,
			admission.UnitOutputTokens: limit * 500, admission.UnitCostMicros: limit * 5000,
		},
		StartsAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	budget, err = budget.Seal(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := live.store.PutEntitlement(context.Background(), entitlement); err != nil {
		t.Fatal(err)
	}
	if err := live.store.PutBudget(context.Background(), budget); err != nil {
		t.Fatal(err)
	}
	return route
}

func liveAdmitRequest(route admission.GatewayRoute, key, payload string, ttl time.Duration) admission.AdmitRequest {
	return admission.AdmitRequest{
		Idempotency: idempotency.Identity{
			Scope: idempotency.MustParseScope("research-team/mlflow-gateway/service-account"),
			Key:   idempotency.MustParseKey(key),
		},
		RequestDigest: identifiers.SHA256String(payload), Subject: "service-account", Workspace: "research-team",
		Route: route, PolicyEpoch: 7,
		Requested: admission.Quota{
			admission.UnitRequests: 1, admission.UnitInputTokens: 100,
			admission.UnitOutputTokens: 50, admission.UnitCostMicros: 500,
		},
		TTL: ttl,
	}
}

func TestLivePostgresAdmissionRoundTripIsAtomicAndRedacted(t *testing.T) {
	live := newLiveAdmissionStore(t)
	route := live.seedPolicy(t, 2)
	request := liveAdmitRequest(route, "request-live-0001", "private-provider-payload", time.Minute)

	decision, err := live.service.Admit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := live.service.Admit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Reservation.ID.String() != decision.Reservation.ID.String() {
		t.Fatalf("idempotent replay=%+v, original=%s", replayed, decision.Reservation.ID)
	}
	committed, err := live.service.Commit(context.Background(), decision.Reservation.ID,
		decision.Reservation.Version, request.RequestDigest, request.Subject,
		admission.Quota{admission.UnitRequests: 1, admission.UnitInputTokens: 90, admission.UnitOutputTokens: 40, admission.UnitCostMicros: 450})
	if err != nil {
		t.Fatal(err)
	}
	if committed.Reservation.State != admission.ReservationCommitted || committed.Reservation.Version.Generation() != 2 {
		t.Fatalf("committed reservation=%+v", committed.Reservation)
	}
	stored, err := live.store.Get(context.Background(), decision.Reservation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != admission.ReservationCommitted || !stored.Actual.Equal(committed.Reservation.Actual) {
		t.Fatalf("stored reservation=%+v", stored)
	}

	for table, want := range map[string]int64{live.auditTable: 4, live.outboxTable: 4, live.reservationTable: 1} {
		var count int64
		if err := live.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("%s count=%d, want %d", table, count, want)
		}
	}
	var payload []byte
	if err := live.db.QueryRow("SELECT payload FROM "+live.outboxTable+" WHERE topic=$1 ORDER BY created_at DESC LIMIT 1",
		"control.admission.reservation.v1").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{request.RequestDigest.String(), request.Idempotency.Key.String(), "private-provider-payload"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("reservation event exposed ownership proof or provider payload: %q", forbidden)
		}
	}
	var policyLineageHeaders int64
	if err := live.db.QueryRow("SELECT count(*) FROM "+live.outboxTable+
		" WHERE topic <> $1 AND headers ? $2", ReservationEventTopic, LineageAuditEventIDHeader).Scan(&policyLineageHeaders); err != nil {
		t.Fatal(err)
	}
	if policyLineageHeaders != 0 {
		t.Fatalf("non-reservation outbox rows with reservation lineage headers = %d", policyLineageHeaders)
	}
}

func TestLivePostgresAdmissionOutboxFailureRollsBackMutationAndAudit(t *testing.T) {
	live := newLiveAdmissionStore(t)
	route := live.seedPolicy(t, 1)
	request := liveAdmitRequest(route, "request-live-rollback-0001", "rollback-payload", time.Minute)

	const constraint = "reject_reservation_events_for_qualification"
	if _, err := live.db.Exec(
		"ALTER TABLE " + live.outboxTable + " ADD CONSTRAINT " + constraint +
			" CHECK (topic <> 'control.admission.reservation.v1')",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := live.service.Admit(context.Background(), request); err == nil {
		t.Fatal("admission unexpectedly succeeded while the durable outbox rejected its event")
	}

	for table, want := range map[string]int64{
		live.auditTable: 2, live.outboxTable: 2, live.reservationTable: 0,
	} {
		var count int64
		if err := live.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("%s count after failed mutation=%d, want %d", table, count, want)
		}
	}

	if _, err := live.db.Exec(
		"ALTER TABLE " + live.outboxTable + " DROP CONSTRAINT " + constraint,
	); err != nil {
		t.Fatal(err)
	}
	decision, err := live.service.Admit(context.Background(), request)
	if err != nil {
		t.Fatalf("same idempotency key was not reusable after rollback: %v", err)
	}
	if decision.Replayed || decision.Reservation.State != admission.ReservationReserved {
		t.Fatalf("post-rollback decision=%+v", decision)
	}
}

func TestLivePostgresAdmissionRetriesSerializationFailure(t *testing.T) {
	live := newLiveAdmissionStore(t)
	route := live.seedPolicy(t, 1)
	request := liveAdmitRequest(route, "request-live-serialization-0001", "serialization-payload", time.Minute)

	schema := strings.TrimSuffix(live.reservationTable, ".gateway_reservations")
	sequence := schema + ".admission_serialization_probe"
	function := schema + ".raise_once_admission_serialization_failure"
	statement := fmt.Sprintf(`
CREATE SEQUENCE %s START 1;
CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF nextval('%s') = 1 THEN
        RAISE EXCEPTION 'qualification serialization failure' USING ERRCODE = '40001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER admission_serialization_probe
BEFORE INSERT ON %s
FOR EACH ROW EXECUTE FUNCTION %s();`, sequence, function, sequence, live.reservationTable, function)
	if _, err := live.db.Exec(statement); err != nil {
		t.Fatal(err)
	}

	decision, err := live.service.Admit(context.Background(), request)
	if err != nil {
		t.Fatalf("admission did not recover from SQLSTATE 40001: %v", err)
	}
	if decision.Replayed || decision.Reservation.State != admission.ReservationReserved {
		t.Fatalf("post-retry decision=%+v", decision)
	}
	var attempts int64
	if err := live.db.QueryRow("SELECT last_value FROM " + sequence).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("serialization attempts=%d, want 2", attempts)
	}
}

func TestLivePostgresAdmissionRejectsNormalizedDocumentDrift(t *testing.T) {
	live := newLiveAdmissionStore(t)
	route := live.seedPolicy(t, 1)
	request := liveAdmitRequest(route, "request-live-generation-0001", "generation-payload", time.Minute)
	decision, err := live.service.Admit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{live.entitlementTable, live.budgetTable, live.reservationTable} {
		if _, err := live.db.Exec(
			"UPDATE " + table + " SET resource_generation = resource_generation + 1",
		); err == nil {
			t.Fatalf("%s accepted resource generation drift from its sealed document", table)
		}
	}
	committed, err := live.service.Commit(
		context.Background(), decision.Reservation.ID, decision.Reservation.Version,
		request.RequestDigest, request.Subject,
		admission.Quota{
			admission.UnitRequests: 1, admission.UnitInputTokens: 90,
			admission.UnitOutputTokens: 40, admission.UnitCostMicros: 450,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := live.db.Exec(
		"UPDATE "+live.reservationTable+" SET finalized_at = finalized_at - interval '1 microsecond' "+
			"WHERE reservation_id = $1",
		committed.Reservation.ID.String(),
	); err == nil {
		t.Fatal("reservation table accepted finalization-time drift from its sealed document")
	}
}

func TestLivePostgresAdmissionConcurrentPressureNeverOverspends(t *testing.T) {
	live := newLiveAdmissionStore(t)
	const (
		budgetLimit = 10
		contenders  = 64
	)
	route := live.seedPolicy(t, budgetLimit)
	start := make(chan struct{})
	errorsByRequest := make(chan error, contenders)
	var successes atomic.Int64
	var exhausted atomic.Int64
	var workers sync.WaitGroup
	workers.Add(contenders)
	for index := 0; index < contenders; index++ {
		index := index
		go func() {
			defer workers.Done()
			<-start
			request := liveAdmitRequest(route, fmt.Sprintf("request-live-%04d", index), fmt.Sprintf("payload-%d", index), time.Minute)
			_, err := live.service.Admit(context.Background(), request)
			switch {
			case err == nil:
				successes.Add(1)
			case faults.IsReason(err, "budget_exhausted"):
				exhausted.Add(1)
			default:
				errorsByRequest <- err
			}
		}()
	}
	close(start)
	workers.Wait()
	close(errorsByRequest)
	for err := range errorsByRequest {
		t.Errorf("unexpected concurrent admission error: %v", err)
	}
	if got := successes.Load(); got != budgetLimit {
		t.Fatalf("successful reservations=%d, want %d; exhausted=%d", got, budgetLimit, exhausted.Load())
	}
	if got := exhausted.Load(); got != contenders-budgetLimit {
		t.Fatalf("exhausted reservations=%d, want %d", got, contenders-budgetLimit)
	}
	var rows, reserved int64
	if err := live.db.QueryRow("SELECT count(*), COALESCE(sum(reserved_requests),0) FROM "+live.reservationTable+
		" WHERE state='reserved'").Scan(&rows, &reserved); err != nil {
		t.Fatal(err)
	}
	if rows != budgetLimit || reserved != budgetLimit {
		t.Fatalf("durable ledger rows=%d reserved=%d, budget=%d", rows, reserved, budgetLimit)
	}
}

func TestLivePostgresAdmissionConcurrentIdempotencyReplaysOneReservation(t *testing.T) {
	live := newLiveAdmissionStore(t)
	route := live.seedPolicy(t, 4)
	request := liveAdmitRequest(route, "request-live-shared-0001", "shared-payload", time.Minute)

	const contenders = 32
	start := make(chan struct{})
	decisions := make(chan admission.Decision, contenders)
	errorsByRequest := make(chan error, contenders)
	var workers sync.WaitGroup
	workers.Add(contenders)
	for range contenders {
		go func() {
			defer workers.Done()
			<-start
			decision, err := live.service.Admit(context.Background(), request)
			if err != nil {
				errorsByRequest <- err
				return
			}
			decisions <- decision
		}()
	}
	close(start)
	workers.Wait()
	close(decisions)
	close(errorsByRequest)
	for err := range errorsByRequest {
		t.Errorf("concurrent idempotent admission failed: %v", err)
	}

	var reservationID string
	created := 0
	count := 0
	for decision := range decisions {
		count++
		if reservationID == "" {
			reservationID = decision.Reservation.ID.String()
		}
		if decision.Reservation.ID.String() != reservationID {
			t.Errorf("concurrent replay returned reservation %s, want %s", decision.Reservation.ID, reservationID)
		}
		if !decision.Replayed {
			created++
		}
	}
	if count != contenders || created != 1 {
		t.Fatalf("successful decisions=%d created=%d, want %d and 1", count, created, contenders)
	}
	for table, want := range map[string]int64{
		live.auditTable: 3, live.outboxTable: 3, live.reservationTable: 1,
	} {
		var rows int64
		if err := live.db.QueryRow("SELECT count(*) FROM " + table).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != want {
			t.Fatalf("%s rows=%d, want %d", table, rows, want)
		}
	}
}

func TestLivePostgresAdmissionExpiryReleasesCapacity(t *testing.T) {
	fake := clock.NewFake(time.Now().Round(0).UTC())
	live := newLiveAdmissionStoreWithClock(t, fake)
	route := live.seedPolicy(t, 1)
	firstRequest := liveAdmitRequest(route, "request-live-expiry-0001", "expiry-one", time.Second)
	first, err := live.service.Admit(context.Background(), firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := fake.Set(first.Reservation.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	expired, err := live.store.ExpireReservations(context.Background(), 10, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 || expired[0].State != admission.ReservationExpired {
		t.Fatalf("expired reservations=%+v", expired)
	}
	secondRequest := liveAdmitRequest(route, "request-live-expiry-0002", "expiry-two", time.Second)
	second, err := live.service.Admit(context.Background(), secondRequest)
	if err != nil {
		t.Fatalf("capacity was not released after materialized expiry: %v", err)
	}
	if second.Reservation.State != admission.ReservationReserved {
		t.Fatalf("second reservation=%+v", second.Reservation)
	}
	var expiredRows, reservedRows int64
	if err := live.db.QueryRow("SELECT count(*) FILTER (WHERE state='expired'), count(*) FILTER (WHERE state='reserved') FROM "+live.reservationTable).
		Scan(&expiredRows, &reservedRows); err != nil {
		t.Fatal(err)
	}
	if expiredRows != 1 || reservedRows != 1 {
		t.Fatalf("expired rows=%d reserved rows=%d", expiredRows, reservedRows)
	}
}
