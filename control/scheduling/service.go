// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

// PlacementQueue is the work queue the scheduler role drains. It matches the
// queue name the composition root configures, so a misconfigured worker drains
// nothing rather than draining someone else's queue.
const PlacementQueue = "control-plane/placement"

// ResultContentType labels the sealed placement a handled item returns.
const ResultContentType = "application/json"

// PlacementCommand is the work item payload.
//
// One command kind, because the placement queue carries one kind of work. A
// polymorphic envelope here would put lifecycle transitions -- bind, complete,
// release -- behind a queue whose worker leases items concurrently, and those
// transitions are ordered per reservation.
type PlacementCommand struct {
	Placement PlacementRequest `json:"placement"`
}

// Decision is the outcome of one durable scheduling mutation.
type Decision struct {
	Reservation Reservation `json:"reservation"`
	Replayed    bool        `json:"replayed"`
}

// Service is the synchronous domain service the scheduler composition root
// injects through WithPlacementHandler.
//
// It is a plain struct with exported fields and no constructor, so a caller
// cannot half-build it behind a builder that silently defaults a collaborator.
// Every method re-checks its dependencies and fails closed: a Service with a
// nil repository or a zero leadership fence refuses to write rather than
// writing without authority.
//
// It starts nothing. No goroutine, no timer, no background reconcile. The
// worker owns concurrency, leasing, and fencing; this owns the decision.
type Service struct {
	// Repository is the durable seam. Required.
	Repository Repository
	// Clock is the time source. A nil Clock means the real clock.
	Clock clock.Clock
	// ReservationTTL bounds a held reservation. Zero means DefaultReservationTTL.
	ReservationTTL time.Duration
	// Fence is the leadership fencing token this process currently holds. Every
	// write carries it, and the store rejects a write whose fence is older than
	// the one it last accepted -- which is what stops a partitioned former
	// leader from reserving capacity after losing the lease.
	Fence uint64
}

var _ workqueue.Handler = Service{}

// Handle turns one claimed work item into a durable capacity reservation. It is
// the seam the scheduler role injects: the root owns the queue, the lease, and
// the worker lifecycle; this owns what the item means.
//
// It is synchronous and total. Every failure is returned, never swallowed: the
// worker's default is to fail an item closed and let it retry, and an
// acknowledged placement that never happened is a workload nobody is waiting on.
func (service Service) Handle(ctx context.Context, item workqueue.Item) (workqueue.Result, error) {
	if err := service.ready(ctx); err != nil {
		return workqueue.Result{}, err
	}
	if err := item.Validate(); err != nil {
		return workqueue.Result{}, err
	}
	if item.Queue != PlacementQueue {
		return workqueue.Result{}, invalid("work_item_queue_mismatch", "work item is not from the placement queue", nil)
	}
	var command PlacementCommand
	decoder := json.NewDecoder(bytes.NewReader(item.Payload))
	// An unknown field is a payload from a different schema version. Ignoring
	// it would place a workload against a request this build only partly
	// understood.
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&command); err != nil {
		return workqueue.Result{}, invalid("placement_command_invalid", "placement command payload is invalid", err)
	}
	if decoder.More() {
		return workqueue.Result{}, invalid("placement_command_invalid", "placement command payload carries trailing content", nil)
	}
	decision, err := service.Place(ctx, command.Placement)
	if err != nil {
		return workqueue.Result{}, err
	}
	payload, err := json.Marshal(decision)
	if err != nil {
		return workqueue.Result{}, unavailable("placement_result_unavailable", "placement result could not be encoded", err)
	}
	if len(payload) > workqueue.MaximumResultBytes {
		return workqueue.Result{}, exhausted("placement_result_bound", "placement result exceeds the work queue result bound")
	}
	return workqueue.Result{ContentType: ResultContentType, Payload: payload}, nil
}

// Place resolves and durably records one placement.
//
// The snapshot is read, the decision is made against that exact snapshot, and
// the store re-checks that the snapshot still describes the fleet inside the
// same critical section as the write. A decision made against a snapshot that
// has since moved is a conflict, not a placement.
func (service Service) Place(ctx context.Context, request PlacementRequest) (Decision, error) {
	if err := service.ready(ctx); err != nil {
		return Decision{}, err
	}
	if err := request.Validate(); err != nil {
		return Decision{}, err
	}
	ttl := service.ReservationTTL
	if ttl == 0 {
		ttl = DefaultReservationTTL
	}
	if ttl < MinimumReservationTTL || ttl > MaximumReservationTTL {
		return Decision{}, invalid("reservation_ttl_invalid", "service reservation TTL is outside bounds", nil)
	}
	now := service.now()
	snapshot, err := service.Repository.Snapshot(ctx, now)
	if err != nil {
		return Decision{}, err
	}
	placement, err := snapshot.Place(request, now)
	if err != nil {
		return Decision{}, err
	}
	reservationID, err := identifiers.NewIDAt(identifiers.MustParseKind("reservation"), now)
	if err != nil {
		return Decision{}, unavailable("reservation_id_unavailable", "reservation ID is unavailable", err)
	}
	candidate, err := NewReservation(reservationID, placement, service.Fence, ttl)
	if err != nil {
		return Decision{}, err
	}
	reservation, replayed, err := service.Repository.Reserve(ctx, snapshot, candidate, now)
	if err != nil {
		return Decision{}, err
	}
	return Decision{Reservation: reservation, Replayed: replayed}, nil
}

// Bind records that the cluster accepted the workload.
func (service Service) Bind(ctx context.Context, id identifiers.ID, expected resourceversion.Version, assignment TopologyAssignment) (Decision, error) {
	if err := service.ready(ctx); err != nil {
		return Decision{}, err
	}
	reservation, replayed, err := service.Repository.Bind(ctx, id, expected, assignment, service.Fence, service.now())
	return Decision{Reservation: reservation, Replayed: replayed}, err
}

// Complete releases capacity after a bound workload finished.
func (service Service) Complete(ctx context.Context, id identifiers.ID, expected resourceversion.Version) (Decision, error) {
	if err := service.ready(ctx); err != nil {
		return Decision{}, err
	}
	reservation, replayed, err := service.Repository.Complete(ctx, id, expected, service.Fence, service.now())
	return Decision{Reservation: reservation, Replayed: replayed}, err
}

// Release returns capacity held for a workload that was never bound.
func (service Service) Release(ctx context.Context, id identifiers.ID, expected resourceversion.Version) (Decision, error) {
	if err := service.ready(ctx); err != nil {
		return Decision{}, err
	}
	reservation, replayed, err := service.Repository.Release(ctx, id, expected, service.Fence, service.now())
	return Decision{Reservation: reservation, Replayed: replayed}, err
}

// Expire returns capacity held past its deadline.
func (service Service) Expire(ctx context.Context, id identifiers.ID, expected resourceversion.Version) (Decision, error) {
	if err := service.ready(ctx); err != nil {
		return Decision{}, err
	}
	reservation, replayed, err := service.Repository.Expire(ctx, id, expected, service.Fence, service.now())
	return Decision{Reservation: reservation, Replayed: replayed}, err
}

// Preempt plans and applies an eviction set for one candidate.
//
// Planning and applying are one call because the plan is only valid against the
// held set it was computed from. Returning a plan for a caller to apply later
// would invite applying it after the set moved, which is how an eviction lands
// on a workload that was never a victim.
func (service Service) Preempt(ctx context.Context, request PreemptionRequest) (PreemptionPlan, []Reservation, error) {
	if err := service.ready(ctx); err != nil {
		return PreemptionPlan{}, nil, err
	}
	if err := request.Validate(); err != nil {
		return PreemptionPlan{}, nil, err
	}
	now := service.now()
	snapshot, err := service.Repository.Snapshot(ctx, now)
	if err != nil {
		return PreemptionPlan{}, nil, err
	}
	share, err := snapshot.FairShare(request.Domain)
	if err != nil {
		return PreemptionPlan{}, nil, err
	}
	held, err := service.Repository.Held(ctx, request.Domain, now)
	if err != nil {
		return PreemptionPlan{}, nil, err
	}
	plan, err := SelectVictims(request, held, share, now)
	if err != nil {
		return PreemptionPlan{}, nil, err
	}
	evicted, _, err := service.Repository.Preempt(ctx, plan, service.Fence, now)
	if err != nil {
		return PreemptionPlan{}, nil, err
	}
	return plan, evicted, nil
}

// ready fails closed on every dependency a write needs.
func (service Service) ready(ctx context.Context) error {
	if ctx == nil {
		return invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if nilInterface(service.Repository) {
		return unavailable("repository_unavailable", "scheduling repository is unavailable", nil)
	}
	if service.Clock != nil && nilInterface(service.Clock) {
		return unavailable("clock_unavailable", "scheduling clock is unavailable", nil)
	}
	// A zero fence is not "no leadership recorded", it is "this process cannot
	// prove it is the leader". Capacity reservations are fleet-wide authority,
	// and granting them without that proof is precisely the split-brain the
	// singleton lease exists to prevent.
	if service.Fence == 0 {
		return failedPrecondition("leadership_fence_missing", "scheduling service holds no leadership fence")
	}
	return nil
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return clock.RealClock{}.Now().Round(0).UTC()
	}
	return service.Clock.Now().Round(0).UTC()
}
