// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import (
	"context"
	"sort"
	"sync"
	"time"

	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

// DefaultReservationBound is the MemoryRepository's ceiling when none is given.
const DefaultReservationBound = 10_000

// Repository is the durable seam. Implementations must make each mutation
// atomic with their audit and outbox writes, and must re-check the capacity
// ledger inside the same transaction as the reservation write -- a snapshot
// read outside the transaction is a suggestion, not a guarantee, and two
// schedulers admitting against the same stale snapshot is exactly how a queue
// gets over-committed.
//
// Every mutation takes the leadership fence and the transaction time
// explicitly. Neither is read from a clock or from ambient state, so a replay
// against a recorded fence and time produces the recorded result.
//
// The bool in each result is "replayed": true when the call found the effect
// already applied and returned it unchanged.
type Repository interface {
	Snapshot(context.Context, time.Time) (FleetSnapshot, error)
	Held(context.Context, CapacityDomain, time.Time) ([]Reservation, error)
	Reserve(context.Context, FleetSnapshot, Reservation, time.Time) (Reservation, bool, error)
	Bind(context.Context, identifiers.ID, resourceversion.Version, TopologyAssignment, uint64, time.Time) (Reservation, bool, error)
	Complete(context.Context, identifiers.ID, resourceversion.Version, uint64, time.Time) (Reservation, bool, error)
	Release(context.Context, identifiers.ID, resourceversion.Version, uint64, time.Time) (Reservation, bool, error)
	Expire(context.Context, identifiers.ID, resourceversion.Version, uint64, time.Time) (Reservation, bool, error)
	Preempt(context.Context, PreemptionPlan, uint64, time.Time) ([]Reservation, bool, error)
	Get(context.Context, identifiers.ID) (Reservation, error)
}

// MemoryRepository is a bounded, concurrency-safe reference implementation.
//
// It lives in production code rather than a _test.go file because it is the
// executable specification of the transaction the SQL implementation owes:
// lazy expiry before every ledger read, fence monotonicity on every write,
// idempotent replay keyed by placement, and a store bound that fails closed.
// A SQL implementation that disagrees with it is wrong.
type MemoryRepository struct {
	mu              sync.RWMutex
	quotas          map[CapacityDomain]Demand
	weights         map[string]uint32
	reservations    map[identifiers.ID]Reservation
	byPlacement     map[string]identifiers.ID
	fence           uint64
	epoch           uint64
	maxReservations int
}

func NewMemoryRepository(maxReservations int) *MemoryRepository {
	if maxReservations <= 0 {
		maxReservations = DefaultReservationBound
	}
	return &MemoryRepository{
		quotas:          make(map[CapacityDomain]Demand, MaximumSnapshotDomains),
		weights:         make(map[string]uint32, MaximumShareClaims),
		reservations:    make(map[identifiers.ID]Reservation, maxReservations),
		byPlacement:     make(map[string]identifiers.ID, maxReservations),
		epoch:           1,
		maxReservations: maxReservations,
	}
}

// checkContext ends a call whose caller has already abandoned it.
//
// ctx.Err() goes back unwrapped, so this adapter carries no reason string here
// while the PostgreSQL store's validate reports scheduling_context_done. That
// is the one reason-string divergence schedulingtest deliberately does not
// assert on, and it is argued at that assertion. The argument is not that the
// domain package has no vocabulary for a context fault -- the branch below
// names context_nil, so plainly it does. It is that no caller can use one: the
// caller handed this error is the caller that cancelled the context, and
// nothing branches on the reason of a call it abandoned itself. Wrapping here
// would mint a domain decision nobody took; making the store match would cost
// the operation metadata an operator reads off a cancelled mutation.
func checkContext(ctx context.Context) error {
	if ctx == nil {
		return invalid("context_nil", "context is required", nil)
	}
	return ctx.Err()
}

// PutQuota records the nominal quota one ClusterQueue grants. Zero quota is a
// valid recorded state and means the queue is held; it is not the same as an
// unrecorded domain, which means nobody measured it.
func (repository *MemoryRepository) PutQuota(ctx context.Context, domain CapacityDomain, nominal Demand) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := nominal.ValidateForDomain(domain, false); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, exists := repository.quotas[domain]; !exists && len(repository.quotas) >= MaximumSnapshotDomains {
		return exhausted("quota_domain_bound", "capacity domain ledger bound was reached")
	}
	// A quota may not be reduced below what is already held. Doing so would
	// leave the ledger permanently over-reserved with no transition able to
	// repair it, and every subsequent Validate would fail.
	held, err := repository.heldLocked(domain)
	if err != nil {
		return err
	}
	if !held.Fits(nominal) {
		return failedPrecondition("quota_below_reserved", "nominal quota cannot be reduced below held capacity")
	}
	repository.quotas[domain] = nominal.Clone()
	repository.epoch++
	return nil
}

// PutWeight records a tenant's fair-share weight.
func (repository *MemoryRepository) PutWeight(ctx context.Context, tenant string, weight uint32) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := validateName(tenant, "tenant"); err != nil {
		return err
	}
	if weight == 0 || weight > MaximumShareWeight {
		return invalid("share_weight_out_of_range", "fair-share weight is outside bounds", nil)
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, exists := repository.weights[tenant]; !exists && len(repository.weights) >= MaximumShareClaims {
		return exhausted("share_claim_bound", "fair-share claim bound was reached")
	}
	repository.weights[tenant] = weight
	repository.epoch++
	return nil
}

// expireLocked applies the deadline to every held reservation. It runs before
// every ledger read, so an expired hold never appears as occupied capacity --
// which is the difference between capacity that is busy and capacity that is
// merely unaccounted for.
func (repository *MemoryRepository) expireLocked(now time.Time) error {
	for id, reservation := range repository.reservations {
		if reservation.State != ReservationHeld || now.Before(reservation.ExpiresAt) {
			continue
		}
		// Expiry is authored by the deadline, not by a leader, so it reuses the
		// reservation's own fence rather than requiring a live one.
		updated, err := reservation.Expire(now, reservation.LeaseFence)
		if err != nil {
			return err
		}
		repository.reservations[id] = updated
	}
	return nil
}

func (repository *MemoryRepository) heldLocked(domain CapacityDomain) (Demand, error) {
	total := Demand(nil)
	for _, reservation := range repository.reservations {
		if reservation.Placement.Pool.Domain != domain {
			continue
		}
		amount := reservation.HeldDemand()
		if amount.IsZero() {
			continue
		}
		next, err := total.add(amount)
		if err != nil {
			return nil, unavailable("capacity_ledger_corrupt", "capacity ledger is invalid", err)
		}
		total = next
	}
	if total == nil {
		return make(Demand), nil
	}
	return total, nil
}

func (repository *MemoryRepository) snapshotLocked(now time.Time) (FleetSnapshot, error) {
	domains := make([]CapacityDomain, 0, len(repository.quotas))
	for domain := range repository.quotas {
		domains = append(domains, domain)
	}
	sort.Slice(domains, func(left, right int) bool { return domains[left].String() < domains[right].String() })

	usage := make(map[CapacityDomain]map[string]Demand, len(domains))
	for _, reservation := range repository.reservations {
		amount := reservation.HeldDemand()
		if amount.IsZero() {
			continue
		}
		domain := reservation.Placement.Pool.Domain
		byTenant, exists := usage[domain]
		if !exists {
			byTenant = make(map[string]Demand, len(repository.weights))
			usage[domain] = byTenant
		}
		next, err := byTenant[reservation.Placement.Tenant].add(amount)
		if err != nil {
			return FleetSnapshot{}, unavailable("capacity_ledger_corrupt", "capacity ledger is invalid", err)
		}
		byTenant[reservation.Placement.Tenant] = next
	}

	allocatables := make([]Allocatable, 0, len(domains))
	shares := make([]FairShare, 0, len(domains))
	for _, domain := range domains {
		reserved, err := repository.heldLocked(domain)
		if err != nil {
			return FleetSnapshot{}, err
		}
		nominal := repository.quotas[domain].Clone()
		allocatable := Allocatable{Domain: domain, Nominal: nominal, Reserved: reserved}
		if err := allocatable.Validate(); err != nil {
			return FleetSnapshot{}, err
		}
		allocatables = append(allocatables, allocatable)

		tenants := make([]string, 0, len(repository.weights))
		for tenant := range repository.weights {
			tenants = append(tenants, tenant)
		}
		sort.Strings(tenants)
		claims := make([]ShareClaim, 0, len(tenants))
		for _, tenant := range tenants {
			used := usage[domain][tenant].Clone()
			if used == nil {
				used = make(Demand)
			}
			claims = append(claims, ShareClaim{Tenant: tenant, Weight: repository.weights[tenant], Used: used})
		}
		share := FairShare{Domain: domain, Capacity: nominal.Clone(), Claims: claims}
		if err := share.Validate(); err != nil {
			return FleetSnapshot{}, err
		}
		shares = append(shares, share)
	}
	snapshot := FleetSnapshot{
		Epoch:          repository.epoch,
		ObservedAt:     now,
		Allocatables:   allocatables,
		Shares:         shares,
		TopologyDigest: TopologyFingerprint(),
	}
	if err := snapshot.Validate(); err != nil {
		return FleetSnapshot{}, err
	}
	return snapshot, nil
}

func (repository *MemoryRepository) Snapshot(ctx context.Context, now time.Time) (FleetSnapshot, error) {
	if err := checkContext(ctx); err != nil {
		return FleetSnapshot{}, err
	}
	if now.IsZero() {
		return FleetSnapshot{}, invalid("snapshot_time_invalid", "snapshot time is required", nil)
	}
	now = now.Round(0).UTC()
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if err := repository.expireLocked(now); err != nil {
		return FleetSnapshot{}, err
	}
	return repository.snapshotLocked(now)
}

func (repository *MemoryRepository) Held(ctx context.Context, domain CapacityDomain, now time.Time) ([]Reservation, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := domain.Validate(); err != nil {
		return nil, err
	}
	if now.IsZero() {
		return nil, invalid("snapshot_time_invalid", "snapshot time is required", nil)
	}
	now = now.Round(0).UTC()
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if err := repository.expireLocked(now); err != nil {
		return nil, err
	}
	active := make([]Reservation, 0, len(repository.reservations))
	for _, reservation := range repository.reservations {
		if reservation.Placement.Pool.Domain == domain && reservation.Active(now) {
			active = append(active, reservation.clone())
		}
	}
	sort.Slice(active, func(left, right int) bool { return active[left].ID.String() < active[right].ID.String() })
	return active, nil
}

func (repository *MemoryRepository) Reserve(ctx context.Context, snapshot FleetSnapshot, candidate Reservation, now time.Time) (Reservation, bool, error) {
	if err := checkContext(ctx); err != nil {
		return Reservation{}, false, err
	}
	if err := snapshot.Validate(); err != nil {
		return Reservation{}, false, err
	}
	if err := candidate.Validate(); err != nil {
		return Reservation{}, false, err
	}
	if candidate.State != ReservationHeld || candidate.Sequence != 0 {
		return Reservation{}, false, invalid("reservation_initial_state_invalid", "reservation is not in its initial state", nil)
	}
	if now.IsZero() {
		return Reservation{}, false, invalid("snapshot_time_invalid", "reservation time is required", nil)
	}
	now = now.Round(0).UTC()
	if !candidate.CreatedAt.Equal(now) {
		return Reservation{}, false, invalid("reservation_created_at_invalid", "reservation creation time does not match the transaction time", nil)
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if err := repository.expireLocked(now); err != nil {
		return Reservation{}, false, err
	}

	key := PlacementKey(candidate.Placement)
	if existingID, exists := repository.byPlacement[key]; exists {
		existing, found := repository.reservations[existingID]
		if !found {
			return Reservation{}, false, unavailable("placement_index_corrupt", "placement index references a missing reservation", nil)
		}
		if !existing.SamePlacement(candidate) {
			return Reservation{}, false, conflict("placement_key_reused", "placement key was reused for a different placement")
		}
		if existing.State.Terminal() {
			return Reservation{}, false, failedPrecondition("reservation_terminal", "the reservation for this placement is already terminal")
		}
		return existing.clone(), true, nil
	}
	if len(repository.reservations) >= repository.maxReservations {
		return Reservation{}, false, exhausted("reservation_store_bound", "reservation store bound was reached")
	}
	if _, exists := repository.reservations[candidate.ID]; exists {
		return Reservation{}, false, conflict("reservation_id_conflict", "reservation ID already exists")
	}
	if candidate.LeaseFence < repository.fence {
		return Reservation{}, false, conflict("lease_fence_stale", "writer holds an older leadership fence than the store")
	}

	// The snapshot the caller decided against must still describe the store.
	// Comparing fingerprints rather than re-running admission keeps the
	// decision the caller's, while making a stale decision impossible to commit.
	current, err := repository.snapshotLocked(now)
	if err != nil {
		return Reservation{}, false, err
	}
	if !current.Fingerprint().Equal(snapshot.Fingerprint()) {
		return Reservation{}, false, conflict("fleet_snapshot_stale", "fleet snapshot is stale")
	}
	domain := candidate.Placement.Pool.Domain
	allocatable, err := current.Allocatable(domain)
	if err != nil {
		return Reservation{}, false, err
	}
	if _, err := allocatable.Reserve(candidate.Placement.Total); err != nil {
		return Reservation{}, false, err
	}

	repository.fence = candidate.LeaseFence
	repository.reservations[candidate.ID] = candidate.clone()
	repository.byPlacement[key] = candidate.ID
	repository.epoch++
	return candidate.clone(), false, nil
}

func (repository *MemoryRepository) Bind(ctx context.Context, id identifiers.ID, expected resourceversion.Version, assignment TopologyAssignment, fence uint64, now time.Time) (Reservation, bool, error) {
	return repository.transition(ctx, id, expected, fence, now, ReservationBound, assignment, identifiers.ID{})
}

func (repository *MemoryRepository) Complete(ctx context.Context, id identifiers.ID, expected resourceversion.Version, fence uint64, now time.Time) (Reservation, bool, error) {
	return repository.transition(ctx, id, expected, fence, now, ReservationCompleted, TopologyAssignment{}, identifiers.ID{})
}

func (repository *MemoryRepository) Release(ctx context.Context, id identifiers.ID, expected resourceversion.Version, fence uint64, now time.Time) (Reservation, bool, error) {
	return repository.transition(ctx, id, expected, fence, now, ReservationReleased, TopologyAssignment{}, identifiers.ID{})
}

func (repository *MemoryRepository) Expire(ctx context.Context, id identifiers.ID, expected resourceversion.Version, fence uint64, now time.Time) (Reservation, bool, error) {
	return repository.transition(ctx, id, expected, fence, now, ReservationExpired, TopologyAssignment{}, identifiers.ID{})
}

func (repository *MemoryRepository) Preempt(ctx context.Context, plan PreemptionPlan, fence uint64, now time.Time) ([]Reservation, bool, error) {
	if err := checkContext(ctx); err != nil {
		return nil, false, err
	}
	if err := plan.Validate(); err != nil {
		return nil, false, err
	}
	if now.IsZero() {
		return nil, false, invalid("snapshot_time_invalid", "preemption time is required", nil)
	}
	now = now.Round(0).UTC()
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if err := repository.expireLocked(now); err != nil {
		return nil, false, err
	}
	if fence == 0 {
		return nil, false, invalid("lease_fence_invalid", "leadership fence is required", nil)
	}
	if fence < repository.fence {
		return nil, false, conflict("lease_fence_stale", "writer holds an older leadership fence than the store")
	}

	// A plan whose victims are already preempted by this candidate is a replay,
	// not a conflict: the worker crashed between the write and the ack.
	replayed := true
	held := make([]Reservation, 0, len(plan.Victims))
	for _, victim := range plan.Victims {
		reservation, exists := repository.reservations[victim.ReservationID]
		if !exists {
			return nil, false, notFound("preemption_victim_not_found", "preemption victim was not found")
		}
		if reservation.State != ReservationPreempted || reservation.Preemptor != plan.Candidate {
			replayed = false
		}
		held = append(held, reservation)
	}
	if replayed {
		applied := make([]Reservation, 0, len(held))
		for _, reservation := range held {
			applied = append(applied, reservation.clone())
		}
		return applied, true, nil
	}
	evicted, err := ApplyPreemption(plan, held, now, fence)
	if err != nil {
		return nil, false, err
	}
	repository.fence = fence
	applied := make([]Reservation, 0, len(evicted))
	for _, reservation := range evicted {
		repository.reservations[reservation.ID] = reservation.clone()
		applied = append(applied, reservation.clone())
	}
	repository.epoch++
	return applied, false, nil
}

func (repository *MemoryRepository) Get(ctx context.Context, id identifiers.ID) (Reservation, error) {
	if err := checkContext(ctx); err != nil {
		return Reservation{}, err
	}
	if err := validateID(id, "reservation", "reservation_id"); err != nil {
		return Reservation{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	reservation, exists := repository.reservations[id]
	if !exists {
		return Reservation{}, notFound("reservation_not_found", "reservation was not found")
	}
	return reservation.clone(), nil
}

func (repository *MemoryRepository) transition(ctx context.Context, id identifiers.ID, expected resourceversion.Version, fence uint64, now time.Time, target ReservationState, assignment TopologyAssignment, preemptor identifiers.ID) (Reservation, bool, error) {
	if err := checkContext(ctx); err != nil {
		return Reservation{}, false, err
	}
	if err := validateID(id, "reservation", "reservation_id"); err != nil {
		return Reservation{}, false, err
	}
	if err := expected.Validate(); err != nil {
		return Reservation{}, false, invalid("expected_version_invalid", "expected reservation version is invalid", err)
	}
	if fence == 0 {
		return Reservation{}, false, invalid("lease_fence_invalid", "leadership fence is required", nil)
	}
	if now.IsZero() {
		return Reservation{}, false, invalid("snapshot_time_invalid", "transition time is required", nil)
	}
	now = now.Round(0).UTC()
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if err := repository.expireLocked(now); err != nil {
		return Reservation{}, false, err
	}
	if fence < repository.fence {
		return Reservation{}, false, conflict("lease_fence_stale", "writer holds an older leadership fence than the store")
	}
	current, exists := repository.reservations[id]
	if !exists {
		return Reservation{}, false, notFound("reservation_not_found", "reservation was not found")
	}
	if current.State == target {
		return current.clone(), true, nil
	}
	if current.State.Terminal() {
		return Reservation{}, false, failedPrecondition("reservation_terminal", "reservation is already terminal")
	}
	if current.Version.String() != expected.String() {
		return Reservation{}, false, conflict("reservation_version_stale", "reservation version is stale")
	}
	var updated Reservation
	var err error
	switch target {
	case ReservationBound:
		updated, err = current.Bind(now, assignment, fence)
	case ReservationCompleted:
		updated, err = current.Complete(now, fence)
	case ReservationReleased:
		updated, err = current.Release(now, fence)
	case ReservationExpired:
		updated, err = current.Expire(now, fence)
	case ReservationPreempted:
		updated, err = current.Preempt(now, preemptor, fence)
	default:
		err = invalid("reservation_transition_invalid", "reservation transition is invalid", nil)
	}
	if err != nil {
		return Reservation{}, false, err
	}
	repository.fence = fence
	repository.reservations[id] = updated.clone()
	repository.epoch++
	return updated.clone(), false, nil
}
