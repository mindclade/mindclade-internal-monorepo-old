// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package maintenance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/clock"
	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	workqueuememory "go.mindclade.dev/libs/go/coordination/workqueue/memory"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
)

func maintenanceSettings() foundationconfig.MapSource {
	return foundationconfig.MapSource{SourceName: "test", Values: map[string]string{
		"signing.hmac_key": "01234567890123456789012345678901",
		"database.dsn":     "postgres://control:control@127.0.0.1:5432/control?sslmode=require",
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
// nothing, serves nothing, and reaches no cluster.
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
	next, err := expirationWorkID(now.Truncate(expirationScheduleInterval).Add(expirationScheduleInterval))
	if err != nil {
		t.Fatal(err)
	}
	if next.String() == id.String() {
		t.Fatal("adjacent schedule buckets produced the same identity")
	}
}
