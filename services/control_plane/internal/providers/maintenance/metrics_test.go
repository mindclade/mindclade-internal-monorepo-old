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
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/storage/sql/sqltest"
	admissionstore "go.mindclade.dev/services/control_plane/internal/store/postgres/admission"
)

type fakeMaintenanceSnapshotSource struct {
	expiration    maintenanceExpirationSnapshot
	expirationErr error
	lineage       maintenanceLineageSnapshot
	lineageErr    error
}

func (source *fakeMaintenanceSnapshotSource) Expiration(context.Context, time.Time) (maintenanceExpirationSnapshot, error) {
	return source.expiration, source.expirationErr
}

func (source *fakeMaintenanceSnapshotSource) Lineage(context.Context, time.Time) (maintenanceLineageSnapshot, error) {
	return source.lineage, source.lineageErr
}

func testMaintenanceMetricsRuntime(t *testing.T, source maintenanceSnapshotSource, value clock.Clock, config maintenanceMetricsConfig) *maintenanceMetricsRuntime {
	t.Helper()
	runtime, err := newMaintenanceMetricsWithConfig("127.0.0.1:0", time.Second, source, value, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close maintenance metrics: %v", err)
		}
	})
	return runtime
}

func TestMaintenanceMetricsExportFixedAtomicSnapshot(t *testing.T) {
	now := time.Date(2026, time.August, 21, 14, 0, 0, 0, time.UTC)
	value := clock.NewFake(now)
	source := &fakeMaintenanceSnapshotSource{
		expiration: maintenanceExpirationSnapshot{
			backlog: 7, oldestExpiredAt: now.Add(-5 * time.Minute),
			lastSuccessfulSweep: now.Add(-10 * time.Second), consecutiveBackloggedSweeps: 2,
		},
		lineage: maintenanceLineageSnapshot{missingAudit: 1, missingOutbox: 2, mismatch: 3},
	}
	runtime := testMaintenanceMetricsRuntime(t, source, value, maintenanceMetricsConfig{
		sampleInterval: time.Second, queryTimeout: 100 * time.Millisecond, staleAfter: 5 * time.Second,
	})
	runtime.sample(context.Background())
	body := scrapeMaintenanceMetrics(t, runtime, http.MethodGet, maintenanceMetricsPath)

	assertMaintenanceMetric(t, body, "mindclade_control_admission_expiration_backlog", "", 7)
	assertMaintenanceMetric(t, body, "mindclade_control_admission_oldest_expired_reservation_age_seconds", "", 300)
	assertMaintenanceMetric(t, body, "mindclade_control_admission_last_successful_sweep_timestamp_seconds", "", unixSeconds(now.Add(-10*time.Second)))
	assertMaintenanceMetric(t, body, "mindclade_control_admission_consecutive_backlogged_sweeps", "", 2)
	assertMaintenanceMetric(t, body, "mindclade_control_admission_event_drift", `{kind="missing_audit"}`, 1)
	assertMaintenanceMetric(t, body, "mindclade_control_admission_event_drift", `{kind="missing_outbox"}`, 2)
	assertMaintenanceMetric(t, body, "mindclade_control_admission_event_drift", `{kind="mismatch"}`, 3)
	for _, probe := range maintenanceProbeNames {
		assertMaintenanceMetric(t, body, "mindclade_control_admission_snapshot_success", `{probe="`+probe+`"}`, 1)
		assertMaintenanceMetric(t, body, "mindclade_control_admission_snapshot_last_success_timestamp_seconds", `{probe="`+probe+`"}`, unixSeconds(now))
	}
	assertMaintenanceMetricInventory(t, body)
	for _, forbidden := range []string{"tenant=", "workspace=", "subject=", "route=", "provider=", "model=", "reason=", "request=", "reservation=", "idempotency="} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("maintenance metrics leaked forbidden label %q:\n%s", forbidden, body)
		}
	}
	if err := runtime.readiness(context.Background()); err != nil {
		t.Fatalf("fresh snapshots were not ready: %v", err)
	}
}

func TestMaintenanceMetricsRetainLastKnownValuesButFailClosedOnFailureAndStaleness(t *testing.T) {
	now := time.Date(2026, time.August, 21, 14, 0, 0, 0, time.UTC)
	value := clock.NewFake(now)
	source := &fakeMaintenanceSnapshotSource{
		expiration: maintenanceExpirationSnapshot{backlog: 4, oldestExpiredAt: now.Add(-time.Minute)},
		lineage:    maintenanceLineageSnapshot{missingAudit: 4},
	}
	runtime := testMaintenanceMetricsRuntime(t, source, value, maintenanceMetricsConfig{
		sampleInterval: time.Second, queryTimeout: 100 * time.Millisecond, staleAfter: 5 * time.Second,
	})
	runtime.sample(context.Background())
	source.lineageErr = errors.New("lineage unavailable")
	runtime.sample(context.Background())
	body := scrapeMaintenanceMetrics(t, runtime, http.MethodGet, maintenanceMetricsPath)
	assertMaintenanceMetric(t, body, "mindclade_control_admission_expiration_backlog", "", 4)
	assertMaintenanceMetric(t, body, "mindclade_control_admission_event_drift", `{kind="missing_audit"}`, 4)
	assertMaintenanceMetric(t, body, "mindclade_control_admission_snapshot_success", `{probe="expiration"}`, 1)
	assertMaintenanceMetric(t, body, "mindclade_control_admission_snapshot_success", `{probe="lineage"}`, 0)
	assertMaintenanceMetric(t, body, "mindclade_control_admission_snapshot_last_success_timestamp_seconds", `{probe="lineage"}`, unixSeconds(now))
	if err := runtime.readiness(context.Background()); !faults.IsReason(err, "maintenance_metrics_snapshot_stale") {
		t.Fatalf("one failed snapshot readiness error = %v", err)
	}

	source.expirationErr = errors.New("expiration unavailable")
	runtime.sample(context.Background())
	body = scrapeMaintenanceMetrics(t, runtime, http.MethodGet, maintenanceMetricsPath)
	for _, probe := range maintenanceProbeNames {
		assertMaintenanceMetric(t, body, "mindclade_control_admission_snapshot_success", `{probe="`+probe+`"}`, 0)
		assertMaintenanceMetric(t, body, "mindclade_control_admission_snapshot_last_success_timestamp_seconds", `{probe="`+probe+`"}`, unixSeconds(now))
	}
	if err := runtime.readiness(context.Background()); !faults.IsReason(err, "maintenance_metrics_snapshot_stale") {
		t.Fatalf("failed snapshots readiness error = %v", err)
	}

	source.expirationErr = nil
	source.lineageErr = nil
	runtime.sample(context.Background())
	if err := value.Advance(6 * time.Second); err != nil {
		t.Fatal(err)
	}
	body = scrapeMaintenanceMetrics(t, runtime, http.MethodGet, maintenanceMetricsPath)
	for _, probe := range maintenanceProbeNames {
		assertMaintenanceMetric(t, body, "mindclade_control_admission_snapshot_success", `{probe="`+probe+`"}`, 0)
	}
	if err := runtime.readiness(context.Background()); !faults.IsReason(err, "maintenance_metrics_snapshot_stale") {
		t.Fatalf("stale snapshots readiness error = %v", err)
	}
}

type sweepCompletingDuringProbeSource struct {
	clock *clock.FakeClock
}

func (source sweepCompletingDuringProbeSource) Expiration(context.Context, time.Time) (maintenanceExpirationSnapshot, error) {
	if err := source.clock.Advance(2 * time.Second); err != nil {
		return maintenanceExpirationSnapshot{}, err
	}
	return maintenanceExpirationSnapshot{lastSuccessfulSweep: source.clock.Now()}, nil
}

func (sweepCompletingDuringProbeSource) Lineage(context.Context, time.Time) (maintenanceLineageSnapshot, error) {
	return maintenanceLineageSnapshot{}, nil
}

func TestMaintenanceMetricsAcceptSweepThatCompletesDuringExpirationQuery(t *testing.T) {
	startedAt := time.Date(2026, time.August, 21, 14, 0, 0, 0, time.UTC)
	value := clock.NewFake(startedAt)
	runtime := testMaintenanceMetricsRuntime(t, sweepCompletingDuringProbeSource{clock: value}, value, maintenanceMetricsConfig{
		sampleInterval: time.Second, queryTimeout: 100 * time.Millisecond, staleAfter: 5 * time.Second,
	})
	runtime.sample(context.Background())
	body := scrapeMaintenanceMetrics(t, runtime, http.MethodGet, maintenanceMetricsPath)
	assertMaintenanceMetric(t, body, "mindclade_control_admission_last_successful_sweep_timestamp_seconds", "", unixSeconds(startedAt.Add(2*time.Second)))
	for _, probe := range maintenanceProbeNames {
		assertMaintenanceMetric(t, body, "mindclade_control_admission_snapshot_success", `{probe="`+probe+`"}`, 1)
	}
	if err := runtime.readiness(context.Background()); err != nil {
		t.Fatalf("concurrent sweep completion made the snapshot unready: %v", err)
	}
}

func TestExpirationBacklogUsesExplicitOverflowSentinel(t *testing.T) {
	now := time.Now().UTC().Round(0)
	valid := maintenanceExpirationSnapshot{
		backlog: expirationBacklogOverflowSentinel, oldestExpiredAt: now.Add(-time.Second),
	}
	if err := validateExpirationSnapshot(valid, now); err != nil {
		t.Fatalf("overflow sentinel was rejected: %v", err)
	}
	valid.backlog++
	if err := validateExpirationSnapshot(valid, now); !faults.IsReason(err, "maintenance_expiration_snapshot_invalid") {
		t.Fatalf("value beyond overflow sentinel error = %v", err)
	}
}

type contextBlockingMaintenanceSource struct{}

func (contextBlockingMaintenanceSource) Expiration(ctx context.Context, _ time.Time) (maintenanceExpirationSnapshot, error) {
	<-ctx.Done()
	return maintenanceExpirationSnapshot{}, ctx.Err()
}

func (contextBlockingMaintenanceSource) Lineage(ctx context.Context, _ time.Time) (maintenanceLineageSnapshot, error) {
	<-ctx.Done()
	return maintenanceLineageSnapshot{}, ctx.Err()
}

func TestMaintenanceMetricsBoundEachPostgresProbeByTimeout(t *testing.T) {
	runtime := testMaintenanceMetricsRuntime(t, contextBlockingMaintenanceSource{}, clock.RealClock{}, maintenanceMetricsConfig{
		sampleInterval: 30 * time.Millisecond, queryTimeout: 20 * time.Millisecond, staleAfter: 100 * time.Millisecond,
	})
	started := time.Now()
	runtime.sample(context.Background())
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("bounded sample took %s", elapsed)
	}
	body := scrapeMaintenanceMetrics(t, runtime, http.MethodGet, maintenanceMetricsPath)
	for _, probe := range maintenanceProbeNames {
		assertMaintenanceMetric(t, body, "mindclade_control_admission_snapshot_success", `{probe="`+probe+`"}`, 0)
	}
}

func TestMaintenanceMetricsHandlerAllowsOnlyExactGetAndHead(t *testing.T) {
	now := time.Now().UTC()
	runtime := testMaintenanceMetricsRuntime(t, &fakeMaintenanceSnapshotSource{}, clock.NewFake(now), maintenanceMetricsConfig{
		sampleInterval: time.Second, queryTimeout: 100 * time.Millisecond, staleAfter: 5 * time.Second,
	})
	get := httptest.NewRecorder()
	runtime.handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, maintenanceMetricsPath, nil))
	if get.Code != http.StatusOK || get.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("GET /metrics = %d headers=%v", get.Code, get.Header())
	}
	head := httptest.NewRecorder()
	runtime.handler.ServeHTTP(head, httptest.NewRequest(http.MethodHead, maintenanceMetricsPath, nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD /metrics = %d body=%q", head.Code, head.Body.String())
	}
	post := httptest.NewRecorder()
	runtime.handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, maintenanceMetricsPath, nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST /metrics = %d allow=%q", post.Code, post.Header().Get("Allow"))
	}
	wrong := httptest.NewRecorder()
	runtime.handler.ServeHTTP(wrong, httptest.NewRequest(http.MethodGet, maintenanceMetricsPath+"/extra", nil))
	if wrong.Code != http.StatusNotFound {
		t.Fatalf("GET wrong path = %d", wrong.Code)
	}
}

func TestMaintenanceMetricsListenerConflictFailsClosed(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, err = newMaintenanceMetrics(listener.Addr().String(), time.Second,
		&fakeMaintenanceSnapshotSource{}, clock.RealClock{})
	if !faults.IsReason(err, "maintenance_metrics_listener_failed") {
		t.Fatalf("occupied metrics address error = %v", err)
	}
}

type countingMaintenanceSnapshotSource struct {
	calls atomic.Int64
}

func (source *countingMaintenanceSnapshotSource) Expiration(context.Context, time.Time) (maintenanceExpirationSnapshot, error) {
	source.calls.Add(1)
	return maintenanceExpirationSnapshot{}, nil
}

func (source *countingMaintenanceSnapshotSource) Lineage(context.Context, time.Time) (maintenanceLineageSnapshot, error) {
	source.calls.Add(1)
	return maintenanceLineageSnapshot{}, nil
}

func TestMaintenanceMetricsScrapePathNeverQueriesPostgres(t *testing.T) {
	now := time.Now().UTC().Round(0)
	source := &countingMaintenanceSnapshotSource{}
	runtime := testMaintenanceMetricsRuntime(t, source, clock.NewFake(now), maintenanceMetricsConfig{
		sampleInterval: time.Second, queryTimeout: 100 * time.Millisecond, staleAfter: 5 * time.Second,
	})
	for range 10 {
		_ = scrapeMaintenanceMetrics(t, runtime, http.MethodGet, maintenanceMetricsPath)
	}
	if calls := source.calls.Load(); calls != 0 {
		t.Fatalf("scrape path invoked %d PostgreSQL probes", calls)
	}
	runtime.sample(context.Background())
	if calls := source.calls.Load(); calls != 2 {
		t.Fatalf("background sample invoked %d probes, want 2", calls)
	}
	_ = scrapeMaintenanceMetrics(t, runtime, http.MethodGet, maintenanceMetricsPath)
	if calls := source.calls.Load(); calls != 2 {
		t.Fatalf("post-sample scrape invoked PostgreSQL probes: %d", calls)
	}
}

func TestPostgresMaintenanceSnapshotUsesBoundedScalarQueries(t *testing.T) {
	now := time.Date(2026, time.August, 21, 14, 0, 0, 0, time.UTC)
	resultPayload := func(backlog bool) []byte {
		payload, err := json.Marshal(housekeepingResult{
			SchemaVersion: housekeepingSchemaVersion, Operation: expireReservationsOperation, Expired: 256, Backlog: backlog,
		})
		if err != nil {
			t.Fatal(err)
		}
		return payload
	}
	queryIndex := 0
	state := &sqltest.State{Query: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
		queryIndex++
		switch queryIndex {
		case 1:
			if strings.Contains(query, "document") || !strings.Contains(query, "ORDER BY expires_at,reservation_id LIMIT $2") ||
				len(arguments) != 2 || arguments[1].Value != int64(expirationBacklogOverflowSentinel) {
				t.Fatalf("unbounded expiration query: %s args=%v", query, arguments)
			}
			return sqltest.NewRows([]string{"count", "oldest"}, []driver.Value{int64(3), now.Add(-3 * time.Minute)}), nil
		case 2:
			if strings.Contains(query, "payload JSON") || !strings.Contains(query, "octet_length(result_payload) <= $2") ||
				!strings.Contains(query, "ORDER BY completed_at DESC,item_id DESC LIMIT 2") || len(arguments) != 2 ||
				arguments[1].Value != int64(maximumHousekeepingResultBytes) {
				t.Fatalf("unbounded sweep query: %s args=%v", query, arguments)
			}
			return sqltest.NewRows([]string{"completed_at", "result_payload"},
				[]driver.Value{now.Add(-time.Second), resultPayload(true)},
				[]driver.Value{now.Add(-2 * time.Second), resultPayload(false)}), nil
		case 3:
			for _, required := range []string{
				"recent_outbox AS MATERIALIZED", "recent_audit AS MATERIALIZED", "LIMIT $2", "headers->>'audit-event-id'",
				"ledger_resource_version", "resource_version !~ '^rv1:", "schema_version IS DISTINCT FROM '1'",
			} {
				if !strings.Contains(query, required) {
					t.Fatalf("lineage query lacks %q: %s", required, query)
				}
			}
			for _, forbidden := range []string{"event_json", "payload FROM"} {
				if strings.Contains(query, forbidden) {
					t.Fatalf("lineage query scans %q: %s", forbidden, query)
				}
			}
			if len(arguments) != 2 || arguments[1].Value != int64(maintenanceDriftLimit) {
				t.Fatalf("lineage arguments=%v", arguments)
			}
			return sqltest.NewRows([]string{"missing_audit", "missing_outbox", "mismatch"}, []driver.Value{int64(1), int64(2), int64(3)}), nil
		default:
			return nil, fmt.Errorf("unexpected query %d: %s", queryIndex, query)
		}
	}}
	database, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	source, err := newPostgresMaintenanceSnapshotSource(database, maintenanceMetricTables{
		reservations: "control.reservations", audit: "control.audit", outbox: "control.outbox", workQueue: "control.work_items",
	})
	if err != nil {
		t.Fatal(err)
	}
	expiration, err := source.Expiration(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if expiration.backlog != 3 || !expiration.oldestExpiredAt.Equal(now.Add(-3*time.Minute)) ||
		!expiration.lastSuccessfulSweep.Equal(now.Add(-time.Second)) || expiration.consecutiveBackloggedSweeps != 1 {
		t.Fatalf("expiration snapshot=%+v", expiration)
	}
	lineage, err := source.Lineage(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if lineage != (maintenanceLineageSnapshot{missingAudit: 1, missingOutbox: 2, mismatch: 3}) {
		t.Fatalf("lineage snapshot=%+v", lineage)
	}
}

func TestPostgresMaintenanceSnapshotFailsClosedWhenSweepResultExceedsBound(t *testing.T) {
	now := time.Date(2026, time.August, 21, 14, 0, 0, 0, time.UTC)
	queryIndex := 0
	state := &sqltest.State{Query: func(_ context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
		queryIndex++
		switch queryIndex {
		case 1:
			return sqltest.NewRows([]string{"count", "oldest"}, []driver.Value{int64(0), nil}), nil
		case 2:
			if !strings.Contains(query, "octet_length(result_payload) <= $2") || len(arguments) != 2 ||
				arguments[1].Value != int64(maximumHousekeepingResultBytes) {
				t.Fatalf("sweep result is not byte-bounded: %s args=%v", query, arguments)
			}
			// PostgreSQL returns NULL for an oversized row through the CASE
			// expression, so the application fails closed without receiving or
			// decoding the oversized bytea value.
			return sqltest.NewRows([]string{"completed_at", "result_payload"}, []driver.Value{now, nil}), nil
		default:
			return nil, fmt.Errorf("unexpected query %d: %s", queryIndex, query)
		}
	}}
	database, err := sqltest.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	source, err := newPostgresMaintenanceSnapshotSource(database, maintenanceMetricTables{
		reservations: "control.reservations", audit: "control.audit", outbox: "control.outbox", workQueue: "control.work_items",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Expiration(context.Background(), now); !faults.IsReason(err, "maintenance_sweep_result_invalid") {
		t.Fatalf("oversized sweep result error = %v", err)
	}
}

func TestLivePostgresMaintenanceSnapshotsDetectRepresentativeDrift(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		t.Fatalf("connect to live PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("mc_mm_%d_%d", os.Getpid(), liveMaintenanceMetricsSchemaSequence.Add(1))
	if _, err := database.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, _ = database.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
		cleanupCancel()
		_ = database.Close()
	})
	tables := maintenanceMetricTables{
		reservations: schema + ".reservations", audit: schema + ".audit_events",
		outbox: schema + ".outbox", workQueue: schema + ".work_items",
	}
	baseDDL := fmt.Sprintf(`CREATE TABLE %s (reservation_id TEXT PRIMARY KEY,state TEXT NOT NULL,expires_at TIMESTAMPTZ NOT NULL,resource_version TEXT NOT NULL);
CREATE TABLE %s (event_id TEXT PRIMARY KEY,occurred_at TIMESTAMPTZ NOT NULL,action TEXT NOT NULL,target_type TEXT NOT NULL,target_id TEXT);
CREATE TABLE %s (message_id TEXT PRIMARY KEY,topic TEXT NOT NULL,headers JSONB NOT NULL,created_at TIMESTAMPTZ NOT NULL);
CREATE TABLE %s (item_id TEXT PRIMARY KEY,queue TEXT NOT NULL,state TEXT NOT NULL,completed_at TIMESTAMPTZ,result_payload BYTEA);`,
		tables.reservations, tables.audit, tables.outbox, tables.workQueue)
	if _, err := database.ExecContext(ctx, baseDDL); err != nil {
		t.Fatal(err)
	}
	observabilityDDL, err := admissionstore.ObservabilityDDL(tables.reservations, tables.audit, tables.outbox, tables.workQueue)
	if err != nil {
		t.Fatal(err)
	}
	workIndex := fmt.Sprintf("CREATE INDEX %s ON %s(queue,completed_at,item_id) WHERE state IN ('completed','failed','cancelled');", schema+"_work_terminal_idx", tables.workQueue)
	if _, err := database.ExecContext(ctx, observabilityDDL+"\n"+workIndex); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 21, 14, 0, 0, 0, time.UTC)
	resourceVersion1 := "rv1:1:sha256:" + strings.Repeat("a", 64)
	resourceVersion2 := "rv1:2:sha256:" + strings.Repeat("b", 64)
	if _, err := database.ExecContext(ctx, "INSERT INTO "+tables.reservations+" VALUES ($1,$2,$3,$4),($5,$6,$7,$8),($9,$10,$11,$12),($13,$14,$15,$16),($17,$18,$19,$20),($21,$22,$23,$24),($25,$26,$27,$28)",
		"reservation-oldest", "reserved", now.Add(-10*time.Minute), resourceVersion1,
		"reservation-newer", "reserved", now.Add(-2*time.Minute), resourceVersion1,
		"reservation-future", "reserved", now.Add(time.Minute), resourceVersion1,
		"reservation-good", "committed", now.Add(-time.Minute), resourceVersion1,
		"reservation-mismatch", "released", now.Add(-time.Minute), resourceVersion1,
		"reservation-version-mismatch", "committed", now.Add(-time.Minute), resourceVersion2,
		"reservation-malformed-version", "committed", now.Add(-time.Minute), resourceVersion1); err != nil {
		t.Fatal(err)
	}
	sweepPayload := func(backlog bool) []byte {
		payload, marshalErr := json.Marshal(housekeepingResult{SchemaVersion: 1, Operation: expireReservationsOperation, Expired: 256, Backlog: backlog})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return payload
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO "+tables.workQueue+" VALUES ($1,$2,$3,$4,$5),($6,$7,$8,$9,$10),($11,$12,$13,$14,$15)",
		"work-latest", housekeepingQueue, "completed", now.Add(-time.Second), sweepPayload(true),
		"work-previous", housekeepingQueue, "completed", now.Add(-2*time.Second), sweepPayload(true),
		"work-older", housekeepingQueue, "completed", now.Add(-3*time.Second), sweepPayload(false)); err != nil {
		t.Fatal(err)
	}
	// A large run of newer failed/cancelled items is the adversarial shape that
	// makes v13's mixed-terminal index unbounded for a completed-only LIMIT 2
	// probe. V14's completed-only index must exclude all of these entries.
	if _, err := database.ExecContext(ctx, "INSERT INTO "+tables.workQueue+" (item_id,queue,state,completed_at) SELECT 'work-terminal-'||value,$1,CASE WHEN value%2=0 THEN 'failed' ELSE 'cancelled' END,$2::timestamptz+(value||' milliseconds')::interval FROM generate_series(1,2000) value",
		housekeepingQueue, now); err != nil {
		t.Fatal(err)
	}
	for _, event := range []struct {
		id, action, target string
	}{
		{"audit-good", "ai_gateway.reservation.commit", "reservation-good"},
		{"audit-mismatch", "ai_gateway.reservation.release", "reservation-mismatch"},
		{"audit-version-mismatch", "ai_gateway.reservation.commit", "reservation-version-mismatch"},
		{"audit-malformed-version", "ai_gateway.reservation.commit", "reservation-malformed-version"},
		{"audit-missing-outbox", "ai_gateway.reservation.expire", "reservation-missing-outbox"},
	} {
		if _, err := database.ExecContext(ctx, "INSERT INTO "+tables.audit+" VALUES ($1,$2,$3,$4,$5)",
			event.id, now.Add(-time.Minute), event.action, admissionstore.ReservationTargetType, event.target); err != nil {
			t.Fatal(err)
		}
	}
	insertOutbox := func(id string, headers map[string]string) {
		t.Helper()
		payload, marshalErr := json.Marshal(headers)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, execErr := database.ExecContext(ctx, "INSERT INTO "+tables.outbox+" VALUES ($1,$2,$3::jsonb,$4)",
			id, admissionstore.ReservationEventTopic, payload, now.Add(-time.Minute)); execErr != nil {
			t.Fatal(execErr)
		}
	}
	lineageHeaders := func(eventID, action, target, resourceVersion string) map[string]string {
		return map[string]string{
			admissionstore.LineageSchemaVersionHeader:   strconv.Itoa(admissionstore.ReservationEventSchemaVersion),
			admissionstore.LineageAuditEventIDHeader:    eventID,
			admissionstore.LineageAuditActionHeader:     action,
			admissionstore.LineageTargetTypeHeader:      admissionstore.ReservationTargetType,
			admissionstore.LineageTargetIDHeader:        target,
			admissionstore.LineageResourceVersionHeader: resourceVersion,
		}
	}
	insertOutbox("outbox-good", lineageHeaders("audit-good", "ai_gateway.reservation.commit", "reservation-good", resourceVersion1))
	insertOutbox("outbox-missing-audit", lineageHeaders("audit-does-not-exist", "ai_gateway.reservation.expire", "reservation-orphan", resourceVersion1))
	insertOutbox("outbox-mismatch", lineageHeaders("audit-mismatch", "ai_gateway.reservation.commit", "reservation-mismatch", resourceVersion1))
	insertOutbox("outbox-version-mismatch", lineageHeaders("audit-version-mismatch", "ai_gateway.reservation.commit", "reservation-version-mismatch", resourceVersion1))
	insertOutbox("outbox-malformed-version", lineageHeaders("audit-malformed-version", "ai_gateway.reservation.commit", "reservation-malformed-version", "reservation:1"))

	source, err := newPostgresMaintenanceSnapshotSource(database, tables)
	if err != nil {
		t.Fatal(err)
	}
	expiration, err := source.Expiration(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if expiration.backlog != 2 || !expiration.oldestExpiredAt.Equal(now.Add(-10*time.Minute)) ||
		!expiration.lastSuccessfulSweep.Equal(now.Add(-time.Second)) || expiration.consecutiveBackloggedSweeps != 2 {
		t.Fatalf("live expiration snapshot=%+v", expiration)
	}
	lineage, err := source.Lineage(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if lineage != (maintenanceLineageSnapshot{missingAudit: 1, missingOutbox: 1, mismatch: 3}) {
		t.Fatalf("live lineage snapshot=%+v", lineage)
	}
	assertMaintenanceMetricIndexes(t, ctx, database, schema, tables, now)
}

func assertMaintenanceMetricIndexes(t *testing.T, ctx context.Context, database *sql.DB, schema string, tables maintenanceMetricTables, now time.Time) {
	t.Helper()
	connection, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "SET enable_seqscan=off"); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		query string
		args  []any
		index string
	}{
		{"SELECT expires_at FROM " + tables.reservations + " WHERE state='reserved' AND expires_at <= $1 ORDER BY expires_at,reservation_id LIMIT 1", []any{now}, "reservations_expiration_observability_idx"},
		{"SELECT event_id FROM " + tables.audit + " WHERE target_type='gateway_reservation' AND occurred_at >= $1 ORDER BY occurred_at DESC,event_id DESC LIMIT 1000", []any{now.Add(-time.Hour)}, "audit_events_admission_observability_idx"},
		{"SELECT message_id FROM " + tables.outbox + " WHERE topic='control.admission.reservation.v1' AND created_at >= $1 ORDER BY created_at DESC,message_id DESC LIMIT 1000", []any{now.Add(-time.Hour)}, "outbox_admission_recent_idx"},
		{"SELECT message_id FROM " + tables.outbox + " WHERE topic='control.admission.reservation.v1' AND headers ? 'audit-event-id' AND headers->>'audit-event-id'=$1", []any{"audit-good"}, "outbox_admission_audit_event_idx"},
		{"SELECT completed_at,result_payload FROM " + tables.workQueue + " WHERE queue=$1 AND state='completed' ORDER BY completed_at DESC,item_id DESC LIMIT 2", []any{housekeepingQueue}, "work_items_completed_observability_idx"},
	}
	for _, check := range checks {
		rows, err := connection.QueryContext(ctx, "EXPLAIN (COSTS OFF) "+check.query, check.args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			plan.WriteString(line)
			plan.WriteByte('\n')
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(plan.String(), check.index) {
			t.Fatalf("query does not use %s in %s:\n%s", check.index, schema, plan.String())
		}
	}
}

func scrapeMaintenanceMetrics(t *testing.T, runtime *maintenanceMetricsRuntime, method, path string) string {
	t.Helper()
	response := httptest.NewRecorder()
	runtime.handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("%s %s = %d body=%q", method, path, response.Code, response.Body.String())
	}
	return response.Body.String()
}

func assertMaintenanceMetric(t *testing.T, body, name, labels string, want float64) {
	t.Helper()
	prefix := name + labels + " "
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(line, prefix)), 64)
		if err != nil {
			t.Fatalf("parse %s: %v", line, err)
		}
		if value != want {
			t.Fatalf("%s%s = %v, want %v", name, labels, value, want)
		}
		return
	}
	t.Fatalf("metric %s%s absent:\n%s", name, labels, body)
}

func assertMaintenanceMetricInventory(t *testing.T, body string) {
	t.Helper()
	want := map[string]int{
		"mindclade_control_admission_expiration_backlog":                      1,
		"mindclade_control_admission_oldest_expired_reservation_age_seconds":  1,
		"mindclade_control_admission_last_successful_sweep_timestamp_seconds": 1,
		"mindclade_control_admission_consecutive_backlogged_sweeps":           1,
		"mindclade_control_admission_event_drift":                             3,
		"mindclade_control_admission_snapshot_success":                        2,
		"mindclade_control_admission_snapshot_last_success_timestamp_seconds": 2,
	}
	got := make(map[string]int, len(want))
	for _, line := range strings.Split(body, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name := strings.Fields(line)[0]
		if separator := strings.IndexByte(name, '{'); separator >= 0 {
			name = name[:separator]
		}
		got[name]++
	}
	if len(got) != len(want) {
		t.Fatalf("metric family inventory=%v, want %v", got, want)
	}
	for family, cardinality := range want {
		if got[family] != cardinality {
			t.Fatalf("metric family %s cardinality=%d, want %d; inventory=%v", family, got[family], cardinality, got)
		}
	}
}

var liveMaintenanceMetricsSchemaSequence atomic.Uint64
