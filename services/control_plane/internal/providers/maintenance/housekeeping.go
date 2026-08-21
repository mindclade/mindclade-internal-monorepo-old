// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package maintenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
	"time"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/services/control_plane/internal/foundation"
)

const (
	housekeepingSchemaVersion       = 1
	expireReservationsOperation     = "expire_gateway_reservations"
	expirationScheduleInterval      = 5 * time.Second
	expirationBatchSize             = 256
	expirationMaximumBatches        = 16
	expirationMaximumBatchSize      = 1000
	expirationMaximumBatchesPerItem = 64
	expirationWorkMaximumAttempts   = 100
	housekeepingJoinTimeout         = 10 * time.Second
)

type reservationExpirer interface {
	ExpireReservations(context.Context, int, time.Time) ([]admission.Reservation, error)
}

type housekeepingRequest struct {
	SchemaVersion  int    `json:"schema_version"`
	Operation      string `json:"operation"`
	BatchSize      int    `json:"batch_size"`
	MaximumBatches int    `json:"maximum_batches"`
}

type housekeepingResult struct {
	SchemaVersion int    `json:"schema_version"`
	Operation     string `json:"operation"`
	Expired       int    `json:"expired"`
	Backlog       bool   `json:"backlog_remaining"`
}

type gatewayHousekeeper struct {
	repository reservationExpirer
	clock      clock.Clock
}

func newGatewayHousekeeper(repository reservationExpirer, value clock.Clock) (workqueue.Handler, error) {
	if foundation.IsNil(repository) || foundation.IsNil(value) {
		return nil, maintenanceFault(faults.CodeInvalidArgument, "housekeeping_dependencies_missing", "gateway housekeeping dependencies are required", "controlplane.maintenance.newGatewayHousekeeper")
	}
	return &gatewayHousekeeper{repository: repository, clock: value}, nil
}

func (handler *gatewayHousekeeper) Handle(ctx context.Context, item workqueue.Item) (workqueue.Result, error) {
	const operation = "controlplane.maintenance.gatewayHousekeeper.Handle"
	if ctx == nil || handler == nil || foundation.IsNil(handler.repository) || foundation.IsNil(handler.clock) {
		return workqueue.Result{}, maintenanceFault(faults.CodeFailedPrecondition, "housekeeping_unconfigured", "gateway housekeeping is not configured", operation)
	}
	if item.Queue != housekeepingQueue {
		return workqueue.Result{}, maintenanceFault(faults.CodeInvalidArgument, "housekeeping_queue_invalid", "housekeeping item targets the wrong queue", operation)
	}
	var request housekeepingRequest
	decoder := json.NewDecoder(bytes.NewReader(item.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return workqueue.Result{}, maintenanceFault(faults.CodeInvalidArgument, "housekeeping_payload_invalid", "housekeeping payload is invalid", operation)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return workqueue.Result{}, maintenanceFault(faults.CodeInvalidArgument, "housekeeping_payload_invalid", "housekeeping payload is invalid", operation)
	}
	if request.SchemaVersion != housekeepingSchemaVersion || request.Operation != expireReservationsOperation ||
		request.BatchSize <= 0 || request.BatchSize > expirationMaximumBatchSize ||
		request.MaximumBatches <= 0 || request.MaximumBatches > expirationMaximumBatchesPerItem {
		return workqueue.Result{}, maintenanceFault(faults.CodeInvalidArgument, "housekeeping_request_invalid", "housekeeping request is outside its supported contract", operation)
	}

	now := handler.clock.Now().Round(0).UTC()
	expiredCount := 0
	backlog := false
	for batch := 0; batch < request.MaximumBatches; batch++ {
		if err := ctx.Err(); err != nil {
			return workqueue.Result{}, faults.Wrap(err, faults.CodeOf(err), faults.PublicMessageOf(err),
				faults.WithReason("housekeeping_context_done"), faults.WithOperation(operation),
				faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
		}
		expired, err := handler.repository.ExpireReservations(ctx, request.BatchSize, now)
		if err != nil {
			return workqueue.Result{}, err
		}
		if len(expired) > request.BatchSize {
			return workqueue.Result{}, maintenanceFault(faults.CodeInternal, "housekeeping_repository_contract_violated", "housekeeping repository returned an oversized batch", operation)
		}
		for _, reservation := range expired {
			if reservation.State != admission.ReservationExpired {
				return workqueue.Result{}, maintenanceFault(faults.CodeInternal, "housekeeping_repository_contract_violated", "housekeeping repository returned a non-expired reservation", operation)
			}
		}
		expiredCount += len(expired)
		if len(expired) < request.BatchSize {
			backlog = false
			break
		}
		backlog = batch == request.MaximumBatches-1
	}
	payload, err := json.Marshal(housekeepingResult{
		SchemaVersion: housekeepingSchemaVersion,
		Operation:     expireReservationsOperation,
		Expired:       expiredCount,
		Backlog:       backlog,
	})
	if err != nil {
		return workqueue.Result{}, maintenanceFault(faults.CodeInternal, "housekeeping_result_encoding_failed", "housekeeping result could not be encoded", operation)
	}
	return workqueue.Result{ContentType: "application/json", Payload: payload}, nil
}

type housekeepingScheduler struct {
	store   workqueue.Store
	clock   clock.Clock
	retry   *retry.Executor
	running atomic.Bool
	ready   atomic.Bool
}

func newHousekeepingScheduler(store workqueue.Store, value clock.Clock, retries *retry.Executor) (*housekeepingScheduler, error) {
	if foundation.IsNil(store) || foundation.IsNil(value) || retries == nil {
		return nil, maintenanceFault(faults.CodeInvalidArgument, "housekeeping_scheduler_dependencies_missing", "housekeeping scheduler dependencies are required", "controlplane.maintenance.newHousekeepingScheduler")
	}
	return &housekeepingScheduler{store: store, clock: value, retry: retries}, nil
}

func (scheduler *housekeepingScheduler) Run(ctx context.Context) error {
	const operation = "controlplane.maintenance.housekeepingScheduler.Run"
	if ctx == nil || scheduler == nil || foundation.IsNil(scheduler.store) || foundation.IsNil(scheduler.clock) || scheduler.retry == nil {
		return maintenanceFault(faults.CodeFailedPrecondition, "housekeeping_scheduler_unconfigured", "housekeeping scheduler is not configured", operation)
	}
	if !scheduler.running.CompareAndSwap(false, true) {
		return maintenanceFault(faults.CodeFailedPrecondition, "housekeeping_scheduler_already_run", "housekeeping scheduler already ran", operation)
	}
	scheduler.ready.Store(true)
	defer scheduler.ready.Store(false)
	for {
		if ctx.Err() != nil {
			return nil
		}
		now := scheduler.clock.Now().Round(0).UTC()
		if err := scheduler.enqueue(ctx, now); err != nil {
			return err
		}
		next := now.Truncate(expirationScheduleInterval).Add(expirationScheduleInterval)
		if err := scheduler.clock.Sleep(ctx, next.Sub(now)); err != nil {
			return nil
		}
	}
}

func (scheduler *housekeepingScheduler) enqueue(ctx context.Context, now time.Time) error {
	const operation = "controlplane.maintenance.housekeepingScheduler.Enqueue"
	bucket := now.Round(0).UTC().Truncate(expirationScheduleInterval)
	id, err := expirationWorkID(bucket)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(housekeepingRequest{
		SchemaVersion: housekeepingSchemaVersion, Operation: expireReservationsOperation,
		BatchSize: expirationBatchSize, MaximumBatches: expirationMaximumBatches,
	})
	if err != nil {
		return maintenanceFault(faults.CodeInternal, "housekeeping_payload_encoding_failed", "housekeeping payload could not be encoded", operation)
	}
	item := workqueue.Item{
		ID: id, Queue: housekeepingQueue, Payload: payload, AvailableAt: bucket,
		MaxAttempts: expirationWorkMaximumAttempts, CreatedAt: now.Round(0).UTC(),
	}
	if err := item.Validate(); err != nil {
		return err
	}
	_, err = scheduler.retry.Do(ctx, "maintenance.housekeeping.enqueue", func(attemptCtx context.Context, _ retry.Attempt) error {
		enqueueErr := scheduler.store.Enqueue(attemptCtx, item)
		if errors.Is(enqueueErr, workqueue.ErrAlreadyExists) || faults.IsCode(enqueueErr, faults.CodeAlreadyExists) {
			return nil
		}
		return enqueueErr
	})
	return err
}

func expirationWorkID(bucket time.Time) (identifiers.ID, error) {
	milliseconds := bucket.UTC().UnixMilli()
	if milliseconds < 0 || uint64(milliseconds) >= 1<<48 {
		return identifiers.ID{}, maintenanceFault(faults.CodeInvalidArgument, "housekeeping_schedule_time_invalid", "housekeeping schedule time is outside UUIDv7 bounds", "controlplane.maintenance.expirationWorkID")
	}
	digest := sha256.Sum256([]byte(expireReservationsOperation + "/" + bucket.UTC().Format(time.RFC3339Nano)))
	value := uint64(milliseconds)
	uuidBytes := make([]byte, identifiers.UUIDBinaryLength)
	uuidBytes[0] = byte(value >> 40)
	uuidBytes[1] = byte(value >> 32)
	uuidBytes[2] = byte(value >> 24)
	uuidBytes[3] = byte(value >> 16)
	uuidBytes[4] = byte(value >> 8)
	uuidBytes[5] = byte(value)
	copy(uuidBytes[6:], digest[:10])
	uuidBytes[6] = uuidBytes[6]&0x0f | 0x70
	uuidBytes[8] = uuidBytes[8]&0x3f | 0x80
	uuid, err := identifiers.UUIDFromBytes(uuidBytes)
	if err != nil {
		return identifiers.ID{}, err
	}
	return identifiers.IDFromUUID(workqueue.ItemIDKind, uuid)
}

func combinedLeaderWork(worker servicekit.Component, scheduler *housekeepingScheduler) (servicekit.Component, error) {
	if worker.Name == "" || worker.Run == nil || scheduler == nil {
		return servicekit.Component{}, maintenanceFault(faults.CodeInvalidArgument, "housekeeping_component_invalid", "housekeeping leader work is invalid", "controlplane.maintenance.combinedLeaderWork")
	}
	workerRun := worker.Run
	worker.Run = func(ctx context.Context) error {
		group, err := servicekit.NewTaskGroup("maintenance-leader", nil)
		if err != nil {
			return err
		}
		if err := group.Add("worker", servicekit.Task(workerRun)); err != nil {
			return err
		}
		if err := group.Add("scheduler", scheduler.Run); err != nil {
			return err
		}
		if err := group.Start(ctx); err != nil {
			return err
		}
		firstErr := group.WaitFirst(ctx)
		group.Cancel(firstErr)
		joinCtx, cancelJoin := context.WithTimeout(context.Background(), housekeepingJoinTimeout)
		defer cancelJoin()
		if _, joinErr := group.Join(joinCtx); joinErr != nil {
			return joinErr
		}
		if ctx.Err() != nil {
			return nil
		}
		if firstErr != nil {
			return firstErr
		}
		return maintenanceFault(faults.CodeUnavailable, "housekeeping_loop_stopped", "housekeeping loop stopped unexpectedly", "controlplane.maintenance.combinedLeaderWork")
	}
	workerReadiness := worker.Readiness
	worker.Readiness = func(ctx context.Context) error {
		if workerReadiness != nil {
			if err := workerReadiness(ctx); err != nil {
				return err
			}
		}
		if !scheduler.ready.Load() {
			return maintenanceFault(faults.CodeUnavailable, "housekeeping_scheduler_not_ready", "housekeeping scheduler is not ready", "controlplane.maintenance.combinedLeaderWork.Readiness")
		}
		return nil
	}
	return worker, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("unexpected trailing JSON value")
	}
	return err
}

func maintenanceFault(code faults.Code, reason, message, operation string) error {
	return faults.New(code, message, faults.WithReason(reason), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
}
