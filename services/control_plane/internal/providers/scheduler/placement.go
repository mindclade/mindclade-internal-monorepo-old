// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduler

import (
	"context"
	"encoding/json"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/scheduling"
	"go.mindclade.dev/libs/go/coordination/leadership"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/requestmeta"
	"go.mindclade.dev/services/control_plane/internal/foundation"
)

// placementItemMaxAttempts bounds how many times one placement is re-offered
// before the queue dead-letters it. It is a bound rather than a tuning knob:
// an item that has been claimed this many times is not waiting for capacity,
// it is failing the same way every time, and leaving it in the queue forever
// hides that from the operator the dead-letter would tell.
//
// The number is sized against placementFailureDelay: 64 attempts five seconds
// apart is a little over five minutes of retrying, which comfortably outlasts a
// leadership handover, a rolling restart, or a serialization storm, and does
// not outlast a stage nobody is going to admit.
const placementItemMaxAttempts = 64

// placementFairSharePosition is the fair-share position used when projecting a
// priority class onto the queue priority at enqueue time.
//
// Zero, deliberately. A tenant's fair-share position is a property of the fleet
// snapshot the placement is decided against, and that snapshot is read by the
// worker inside Place, not by the producer inside the promotion transaction.
// Reading it here would mean either a second fleet read in the critical section
// of every promotion, or a stale number quoted as though it were current. The
// class alone orders the queue; fairness is applied where the decision is made.
const placementFairSharePosition = 0

// fencedPlacement is the placement handler the role installs by default.
//
// It re-reads the fence on EVERY item rather than capturing one when the
// handler is built. The fence is the durable lease version and the elector
// advances it on every renewal, so a handler that closed over a single
// scheduling.Service value would quote the epoch it was constructed in for the
// whole life of the process. A fence-monotonic store refuses a write whose
// fence is below the highest it has accepted -- that is how a deposed leader is
// stopped -- and a live leader quoting an expired fence is indistinguishable
// from a deposed one. Nothing in the type system checks this: scheduling.Service
// is a value with an exported Fence field, so the stale spelling compiles and
// passes every test that does not renew a lease.
//
// The copy is what makes the per-call write safe. scheduling.Service is a plain
// struct, so `scoped := service` yields this call's own value and the shared one
// is never mutated by a worker goroutine.
func fencedPlacement(service scheduling.Service, view *leadership.SessionView) workqueue.HandlerFunc {
	return func(ctx context.Context, item workqueue.Item) (workqueue.Result, error) {
		scoped := service
		scoped.Fence = view.Fence()
		return scoped.Handle(ctx, item)
	}
}

// PlacementFacts resolves the fleet admission question one promoted stage asks.
//
// # Why this seam exists
//
// orchestration.WorkItem carries identity only -- run, job, stage, attempt --
// because a queue payload is not authority. scheduling.PlacementRequest embeds
// an AdmissionRequest, which additionally names the tenant, the workspace, the
// stage kind, the resource pool, the accelerator, the priority class, the
// per-replica demand, the replica count, the topology constraint, and the
// upstream stages. Those are not derivable from the four identity fields, and
// they are not in orchestration's durable state either: the stage table records
// run, job, stage, state and attempts; the workflow table records the compiled
// graph but nothing links a run to the workflow it was started from; and tenant
// and workspace live only in the caller-held control/ingestion.Plan, never in a
// row. A producer that invented any of them would be charging a fleet-wide
// capacity ledger against a tenant it made up.
//
// So the facts are supplied, not guessed. Admission runs inside the promotion's
// transaction and is handed that transaction's context, so an implementation
// that reads through a store resolving its executor with
// storage/sql/transaction.FromContext reads the same uncommitted state the
// promotion is writing, and rolls back with it.
//
// # What an implementation must not do
//
// It must not call back into the orchestration repository that is mid-mutation:
// orchestration.Enqueuer runs inside that mutation's critical section, which in
// the reference adapter is a held mutex. Reading through a SEPARATE store on
// the same transaction context is the supported shape.
type PlacementFacts interface {
	Admission(context.Context, orchestration.WorkItem) (scheduling.AdmissionRequest, error)
}

// PlacementFactsFunc adapts a function to PlacementFacts.
type PlacementFactsFunc func(context.Context, orchestration.WorkItem) (scheduling.AdmissionRequest, error)

func (function PlacementFactsFunc) Admission(ctx context.Context, item orchestration.WorkItem) (scheduling.AdmissionRequest, error) {
	if function == nil {
		return scheduling.AdmissionRequest{}, placementFault(faults.CodeUnavailable,
			"placement_facts_unavailable", "placement facts source is unavailable",
			"controlplane.scheduler.PlacementFactsFunc.Admission")
	}
	return function(ctx, item)
}

// placementProducer is the orchestration.Enqueuer that turns a promoted stage
// into work on the placement queue.
//
// This is the only object in the repository that legitimately holds both
// domains. control/scheduling already imports control/orchestration for
// StageKind, so the reverse edge would be an import cycle; ADR-0029 states the
// same boundary from the architecture side, and says the two packages
// "communicate only through the control-plane/placement durable work queue".
// The composition root is therefore where the queue name lives and where the
// payloads are translated.
//
// The translation is not optional plumbing. scheduling.Service.Handle rejects
// any item whose Queue is not scheduling.PlacementQueue and decodes the payload
// with DisallowUnknownFields, so a raw orchestration.WorkItem posted onto that
// queue decodes into a PlacementCommand with four unknown fields and is failed
// closed -- retried to its attempt bound and then dead-lettered. The promotion
// would look successful and the stage would never be placed.
type placementProducer struct {
	queue workqueue.Store
	facts PlacementFacts
}

var _ orchestration.Enqueuer = placementProducer{}

// newPlacementProducer binds a placement producer to the queue the scheduler
// role drains.
//
// queue must be the store that resolves its executor from the context --
// coordination/workqueue/postgres does, which is what puts the item in the same
// transaction as the stage transition, its audit record and its outbox message.
// A producer holding an independent handle to the same database would commit
// the item on its own connection: a crash between the two commits then leaves
// either a placement for a promotion that rolled back, or a promoted stage
// nothing will ever place.
func newPlacementProducer(queue workqueue.Store, facts PlacementFacts) (orchestration.Enqueuer, error) {
	if foundation.IsNil(queue) {
		return nil, placementFault(faults.CodeInvalidArgument,
			"placement_queue_missing", "placement producer requires a work queue store",
			"controlplane.scheduler.newPlacementProducer")
	}
	if foundation.IsNil(facts) {
		return nil, placementFault(faults.CodeInvalidArgument,
			"placement_facts_missing", "placement producer requires a placement facts source",
			"controlplane.scheduler.newPlacementProducer")
	}
	return placementProducer{queue: queue, facts: facts}, nil
}

// EnqueueStage translates one promoted stage and appends it to the placement
// queue on the caller's transaction context.
//
// Every failure is returned unchanged where it came from the facts source or
// the queue, because the repository reads its code and retry policy to tell a
// serialization conflict that must be replayed from a payload that never can
// be. Restating them here would flatten that distinction.
func (producer placementProducer) EnqueueStage(ctx context.Context, work orchestration.WorkItem) error {
	const operation = "controlplane.scheduler.placementProducer.EnqueueStage"
	if ctx == nil {
		return placementFault(faults.CodeInvalidArgument, "context_nil", "context is required", operation)
	}
	if err := work.Validate(); err != nil {
		return err
	}
	if foundation.IsNil(producer.queue) || foundation.IsNil(producer.facts) {
		return placementFault(faults.CodeFailedPrecondition, "placement_producer_unconfigured",
			"placement producer is not configured", operation)
	}
	admission, err := producer.facts.Admission(ctx, work)
	if err != nil {
		return err
	}
	request := scheduling.PlacementRequest{
		Admission: admission,
		RunID:     work.RunID,
		StageID:   work.StageID,
		Attempt:   work.Attempt,
	}
	// Validated here rather than left to the worker. A request that cannot be
	// placed is a promotion that must not commit: failing it now rolls the
	// stage back to where a reconciler can retry it, whereas an invalid item
	// on the queue is a stage that is durably queued and permanently
	// unplaceable.
	if err := request.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(scheduling.PlacementCommand{Placement: request})
	if err != nil {
		return placementFault(faults.CodeInternal, "placement_command_encoding_failed",
			"placement command could not be encoded", operation)
	}
	priority, err := scheduling.QueuePriority(admission.Priority, placementFairSharePosition)
	if err != nil {
		return err
	}
	// Lineage travels with the work. The role declares
	// RequestMetadataConfigured, and that claim is only true if the request
	// that caused a promotion is still readable from the item it produced.
	metadata, _ := requestmeta.FromContext(ctx)
	item, err := workqueue.NewItem(
		scheduling.PlacementQueue, payload, priority, time.Time{}, placementItemMaxAttempts, metadata)
	if err != nil {
		return err
	}
	return producer.queue.Enqueue(ctx, item)
}

func placementFault(code faults.Code, reason, message, operation string) error {
	return faults.New(code, message,
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
