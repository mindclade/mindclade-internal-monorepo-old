// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package maintenance

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/audit"
	"go.mindclade.dev/libs/go/clock"
	foundationconfig "go.mindclade.dev/libs/go/config"
	outboxmemory "go.mindclade.dev/libs/go/coordination/outbox/memory"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	workqueuememory "go.mindclade.dev/libs/go/coordination/workqueue/memory"
	workqueuepostgres "go.mindclade.dev/libs/go/coordination/workqueue/postgres"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/storage/sql/sqltest"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	admissionstore "go.mindclade.dev/services/control_plane/internal/store/postgres/admission"
)

const liveMaintenancePostgresEnvironment = "MINDCLADE_TEST_POSTGRES_DSN"

var liveMaintenanceSchemaSequence atomic.Uint64

func maintenanceSettings() foundationconfig.MapSource {
	return foundationconfig.MapSource{SourceName: "test", Values: map[string]string{
		"signing.hmac_key": "01234567890123456789012345678901",
		"database.dsn":     "postgres://control:control@127.0.0.1:5432/control?sslmode=require",
		"metrics.address":  "127.0.0.1:0",
	}}
}

// Building through servicekit/production is the assertion that matters: the
// maintenance role needs a lease, an elector, and a worker in the work stage.
func TestMaintenanceFactoryBuildsThroughProductionLifecycle(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleMaintenance)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewMaintenanceFactory(maintenanceSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	service, err := bootstrap.Build(profile, runtime)
	if err != nil {
		t.Fatalf("maintenance runtime does not satisfy its production role: %v", err)
	}
	if service == nil || service.Service() == nil {
		t.Fatal("production runtime was not assembled")
	}
}

// Maintenance is the narrowest role that still holds a lease. It publishes
// nothing, exposes no application transport, and reaches no cluster. Its
// private metrics listener is auxiliary and claims no HTTP capability.
func TestMaintenanceComposesOnlyWhatItsRoleNeeds(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleMaintenance)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewMaintenanceFactory(maintenanceSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := bootstrap.Capabilities(runtime.Dependencies...)
	present := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		present[capability.String()] = struct{}{}
	}
	for _, absent := range []string{
		"http", "grpc", "connect", "authentication", "authorization",
		"kubernetes", "kubernetes_manager", "messaging",
		"blob_store", "cache", "projector", "cursor_store", "migrations",
		"signing", "pagination",
	} {
		if _, found := present[absent]; found {
			t.Fatalf("maintenance composes %q, which its role does not require", absent)
		}
	}
}

func TestMaintenanceWiresDedicatedAdmissionMetricsLifecycle(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleMaintenance)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewMaintenanceFactory(maintenanceSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	serverFound := false
	for _, auxiliary := range runtime.Components.Auxiliary {
		if auxiliary.Stage == servicekit.StageServing && auxiliary.Component.Name == "admission-maintenance-metrics-server" {
			serverFound = true
		}
	}
	samplerFound := false
	for _, component := range runtime.Components.Work {
		if component.Name == "admission-maintenance-metrics-sampler" && component.Run != nil && component.Readiness != nil {
			samplerFound = true
		}
	}
	if !serverFound || !samplerFound {
		t.Fatalf("maintenance metric lifecycle server=%t sampler=%t", serverFound, samplerFound)
	}
}

func TestLeaderManagedWorkReadinessRequiresAdmissionSchema(t *testing.T) {
	queryCount := 0
	state := &sqltest.State{Query: func(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
		queryCount++
		if queryCount == 2 {
			return nil, errors.New("required budget column is missing")
		}
		return sqltest.NewRows([]string{"shape"}), nil
	}}
	database, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	value := clock.NewFake(now)
	messages, err := outboxmemory.New(outboxmemory.WithClock(value))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := retry.NewPolicy(retry.WithMaxAttempts(1))
	if err != nil {
		t.Fatal(err)
	}
	retries, err := retry.NewExecutor(policy, retry.WithClock(value))
	if err != nil {
		t.Fatal(err)
	}
	admissions, err := admissionstore.New(database, audit.NopRecorder{}, messages,
		admissionstore.WithClock(value), admissionstore.WithRetry(retries))
	if err != nil {
		t.Fatal(err)
	}
	scheduler := newTestHousekeepingScheduler(t, workqueuememory.New(), value)
	scheduler.ready.Store(true)
	component, err := combinedLeaderWork(servicekit.Component{
		Name: "worker/housekeeping", Run: func(context.Context) error { return nil },
		Readiness: func(context.Context) error { return nil },
	}, scheduler, admissions.Component("admission-schema").Readiness)
	if err != nil {
		t.Fatal(err)
	}
	if err := component.Readiness(context.Background()); err == nil {
		t.Fatal("leader-managed work reported ready with a missing admission column")
	}
	if queryCount != 2 {
		t.Fatalf("admission readiness queries = %d, want 2", queryCount)
	}
}

type expirerCall struct {
	limit int
	now   time.Time
}

type fakeExpirer struct {
	calls   []expirerCall
	batches [][]admission.Reservation
	err     error
}

func (expirer *fakeExpirer) ExpireReservations(_ context.Context, limit int, now time.Time) ([]admission.Reservation, error) {
	expirer.calls = append(expirer.calls, expirerCall{limit: limit, now: now})
	if expirer.err != nil {
		return nil, expirer.err
	}
	if len(expirer.batches) == 0 {
		return nil, nil
	}
	result := expirer.batches[0]
	expirer.batches = expirer.batches[1:]
	return result, nil
}

func TestGatewayHousekeepingDrainsBoundedBatches(t *testing.T) {
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	expirer := &fakeExpirer{batches: [][]admission.Reservation{
		{{State: admission.ReservationExpired}, {State: admission.ReservationExpired}},
		{{State: admission.ReservationExpired}},
	}}
	handler, err := newGatewayHousekeeper(expirer, clock.NewFake(now))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(housekeepingRequest{
		SchemaVersion: housekeepingSchemaVersion, Operation: expireReservationsOperation,
		BatchSize: 2, MaximumBatches: 3,
	})
	result, err := handler.Handle(context.Background(), workqueue.Item{Queue: housekeepingQueue, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	var decoded housekeepingResult
	if err := json.Unmarshal(result.Payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if result.ContentType != "application/json" || decoded.Expired != 3 || decoded.Backlog || len(expirer.calls) != 2 {
		t.Fatalf("result=%+v calls=%+v", decoded, expirer.calls)
	}
	for _, call := range expirer.calls {
		if call.limit != 2 || !call.now.Equal(now) {
			t.Fatalf("call=%+v", call)
		}
	}
}

func TestGatewayHousekeepingRejectsContractDrift(t *testing.T) {
	handler, err := newGatewayHousekeeper(&fakeExpirer{}, clock.NewFake(time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string]string{
		"unknown field":   `{"schema_version":1,"operation":"expire_gateway_reservations","batch_size":1,"maximum_batches":1,"extra":true}`,
		"wrong version":   `{"schema_version":2,"operation":"expire_gateway_reservations","batch_size":1,"maximum_batches":1}`,
		"unbounded batch": `{"schema_version":1,"operation":"expire_gateway_reservations","batch_size":1001,"maximum_batches":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := handler.Handle(context.Background(), workqueue.Item{Queue: housekeepingQueue, Payload: json.RawMessage(payload)})
			if !faults.IsCode(err, faults.CodeInvalidArgument) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestRecurringScheduleUsesStableTimeBucketIdentity(t *testing.T) {
	now := time.Date(2026, time.August, 21, 12, 0, 2, 0, time.UTC)
	value := clock.NewFake(now)
	store := workqueuememory.New()
	policy, err := retry.NewPolicy(retry.WithMaxAttempts(1))
	if err != nil {
		t.Fatal(err)
	}
	retries, err := retry.NewExecutor(policy, retry.WithClock(value))
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := newHousekeepingScheduler(store, value, retries)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.enqueue(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	// A restart or leadership reacquisition in the same bucket is an
	// idempotent replay, not another sweep item.
	if err := scheduler.enqueue(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	id, err := expirationWorkID(now.Truncate(expirationScheduleInterval))
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Lookup(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if record.Item.Queue != housekeepingQueue || record.Item.MaxAttempts != expirationWorkMaximumAttempts {
		t.Fatalf("scheduled item=%+v", record.Item)
	}
	if record.Item.Request.RequestID.ID().UUID() != id.UUID() ||
		record.Item.Request.CorrelationID.String() != record.Item.Request.RequestID.String() ||
		record.Item.Request.Operation.String() != "controlplane.maintenance.expire_gateway_reservations" {
		t.Fatalf("scheduled request metadata=%+v", record.Item.Request)
	}
	next, err := expirationWorkID(now.Truncate(expirationScheduleInterval).Add(expirationScheduleInterval))
	if err != nil {
		t.Fatal(err)
	}
	if next.String() == id.String() {
		t.Fatal("adjacent schedule buckets produced the same identity")
	}
}

func TestRecurringScheduleRunsSuccessiveBuckets(t *testing.T) {
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	value := clock.NewFake(now)
	store := workqueuememory.New()
	scheduler := newTestHousekeepingScheduler(t, store, value)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx) }()
	waitCtx, stopWaiting := context.WithTimeout(context.Background(), time.Second)
	defer stopWaiting()
	if err := value.BlockUntil(waitCtx, 1); err != nil {
		t.Fatal(err)
	}
	first, err := expirationWorkID(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Lookup(context.Background(), first); err != nil {
		t.Fatalf("first scheduled bucket: %v", err)
	}
	if err := value.Advance(expirationScheduleInterval); err != nil {
		t.Fatal(err)
	}
	if err := value.BlockUntil(waitCtx, 1); err != nil {
		t.Fatal(err)
	}
	second, err := expirationWorkID(now.Add(expirationScheduleInterval))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Lookup(context.Background(), second); err != nil {
		t.Fatalf("second scheduled bucket: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after cancellation")
	}
}

func TestRecurringScheduleRejectsIdentityPayloadCollision(t *testing.T) {
	now := time.Date(2026, time.August, 21, 12, 0, 2, 0, time.UTC)
	value := clock.NewFake(now)
	store := workqueuememory.New()
	scheduler := newTestHousekeepingScheduler(t, store, value)
	id, err := expirationWorkID(now.Truncate(expirationScheduleInterval))
	if err != nil {
		t.Fatal(err)
	}
	colliding := workqueue.Item{
		ID: id, Queue: housekeepingQueue, Payload: json.RawMessage(`{"schema_version":1,"operation":"different"}`),
		AvailableAt: now.Truncate(expirationScheduleInterval), MaxAttempts: expirationWorkMaximumAttempts, CreatedAt: now,
	}
	if err := store.Enqueue(context.Background(), colliding); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.enqueue(context.Background(), now); !faults.IsCode(err, faults.CodeDataLoss) ||
		faults.ReasonOf(err) != "housekeeping_schedule_collision" {
		t.Fatalf("enqueue collision error = %v, want data-loss housekeeping_schedule_collision", err)
	}
}

func TestRecurringScheduleAcceptsCanonicalizedJSONBReplay(t *testing.T) {
	now := time.Date(2026, time.August, 21, 12, 0, 2, 0, time.UTC)
	bucket := now.Truncate(expirationScheduleInterval)
	value := clock.NewFake(now)
	store := workqueuememory.New()
	scheduler := newTestHousekeepingScheduler(t, store, value)
	id, err := expirationWorkID(bucket)
	if err != nil {
		t.Fatal(err)
	}
	request, err := expirationRequestMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	// PostgreSQL JSONB normalizes insignificant whitespace and object key order before Lookup.
	// Replay equality must therefore bind the decoded closed contract, not source bytes.
	canonicalized := json.RawMessage(`{
  "batch_size": 256,
  "maximum_batches": 16,
  "operation": "expire_gateway_reservations",
  "schema_version": 1
}`)
	if err := store.Enqueue(context.Background(), workqueue.Item{
		ID: id, Queue: housekeepingQueue, Payload: canonicalized, AvailableAt: bucket,
		MaxAttempts: expirationWorkMaximumAttempts, CreatedAt: now, Request: request,
	}); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.enqueue(context.Background(), now); err != nil {
		t.Fatalf("canonical JSONB replay was rejected: %v", err)
	}
}

func TestLivePostgresRecurringScheduleAcceptsJSONBRoundTripReplay(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(liveMaintenancePostgresEnvironment))
	if dsn == "" {
		t.Skipf("%s is not set; live PostgreSQL qualification is opt-in", liveMaintenancePostgresEnvironment)
	}
	database, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		t.Fatalf("connect to live PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("mc_maintenance_qual_%d_%d", os.Getpid(), liveMaintenanceSchemaSequence.Add(1))
	if _, err := database.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		_ = database.Close()
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
		_ = database.Close()
	})
	table := schema + ".work_items"
	ddl, err := workqueuepostgres.DDL(table)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, ddl); err != nil {
		t.Fatalf("apply workqueue DDL: %v", err)
	}
	store, err := workqueuepostgres.New(database, table)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 21, 12, 0, 2, 0, time.UTC)
	scheduler := newTestHousekeepingScheduler(t, store, clock.NewFake(now))
	if err := scheduler.enqueue(ctx, now); err != nil {
		t.Fatalf("initial schedule: %v", err)
	}
	// The second call takes the duplicate-ID Lookup path after PostgreSQL has
	// normalized the JSONB payload. It must remain a semantic replay.
	if err := scheduler.enqueue(ctx, now.Add(time.Second)); err != nil {
		t.Fatalf("same-bucket JSONB replay: %v", err)
	}
	var count int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("scheduled rows = %d, want 1", count)
	}
}

func TestRecurringScheduleRejectsIdentityAvailableAtCollision(t *testing.T) {
	now := time.Date(2026, time.August, 21, 12, 0, 2, 0, time.UTC)
	bucket := now.Truncate(expirationScheduleInterval)
	value := clock.NewFake(now)
	store := workqueuememory.New()
	scheduler := newTestHousekeepingScheduler(t, store, value)
	id, err := expirationWorkID(bucket)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(housekeepingRequest{
		SchemaVersion: housekeepingSchemaVersion, Operation: expireReservationsOperation,
		BatchSize: expirationBatchSize, MaximumBatches: expirationMaximumBatches,
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := expirationRequestMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	colliding := workqueue.Item{
		ID: id, Queue: housekeepingQueue, Payload: payload, AvailableAt: bucket.Add(time.Hour),
		MaxAttempts: expirationWorkMaximumAttempts, CreatedAt: now, Request: request,
	}
	if err := store.Enqueue(context.Background(), colliding); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.enqueue(context.Background(), now); !faults.IsCode(err, faults.CodeDataLoss) ||
		faults.ReasonOf(err) != "housekeeping_schedule_collision" {
		t.Fatalf("enqueue availability collision error = %v, want data-loss housekeeping_schedule_collision", err)
	}
}

func TestRecurringSchedulePrunesOnlyExpiredTerminalHistory(t *testing.T) {
	now := time.Now().UTC().Round(0)
	value := clock.NewFake(now)
	store := workqueuememory.New()
	old := enqueueCompletedHousekeepingItem(t, store, now.Add(-expirationTerminalRetention-time.Hour))
	recent := enqueueCompletedHousekeepingItem(t, store, now.Add(-expirationTerminalRetention+time.Hour))
	scheduler := newTestHousekeepingScheduler(t, store, value)
	if err := scheduler.enqueue(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Lookup(context.Background(), old); !errors.Is(err, workqueue.ErrNotFound) {
		t.Fatalf("old terminal item lookup error = %v, want not found", err)
	}
	if _, err := store.Lookup(context.Background(), recent); err != nil {
		t.Fatalf("recent terminal item was pruned: %v", err)
	}
}

func newTestHousekeepingScheduler(t *testing.T, store workqueue.Store, value *clock.FakeClock) *housekeepingScheduler {
	t.Helper()
	policy, err := retry.NewPolicy(retry.WithMaxAttempts(1))
	if err != nil {
		t.Fatal(err)
	}
	retries, err := retry.NewExecutor(policy, retry.WithClock(value))
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := newHousekeepingScheduler(store, value, retries)
	if err != nil {
		t.Fatal(err)
	}
	return scheduler
}

func enqueueCompletedHousekeepingItem(t *testing.T, store *workqueuememory.Store, completedAt time.Time) identifiers.ID {
	t.Helper()
	id, err := expirationWorkID(completedAt.Truncate(expirationScheduleInterval))
	if err != nil {
		t.Fatal(err)
	}
	item := workqueue.Item{
		ID: id, Queue: housekeepingQueue, Payload: json.RawMessage(`{"schema_version":1,"operation":"expire_gateway_reservations","batch_size":256,"maximum_batches":16}`),
		AvailableAt: time.Now().UTC().Add(-time.Minute), MaxAttempts: expirationWorkMaximumAttempts, CreatedAt: completedAt.Add(-time.Second),
	}
	if err := store.Enqueue(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	claims, err := store.Claim(context.Background(), workqueue.ClaimRequest{
		Owner: "retention-test", Queues: []string{housekeepingQueue}, Limit: 1, LeaseDuration: time.Minute,
	})
	if err != nil || len(claims) != 1 {
		t.Fatalf("Claim() = %d, %v, want 1, nil", len(claims), err)
	}
	if err := store.Complete(context.Background(), claims[0], workqueue.Result{}, completedAt); err != nil {
		t.Fatal(err)
	}
	return id
}
