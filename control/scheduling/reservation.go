// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import (
	"strconv"
	"time"

	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

const (
	// MinimumReservationTTL is the shortest holdable window. Anything shorter
	// expires inside the round trip that would bind it.
	MinimumReservationTTL = time.Second
	// DefaultReservationTTL is the window a service uses when it states none.
	DefaultReservationTTL = 15 * time.Minute
	// MaximumReservationTTL bounds a held reservation. Capacity held past this
	// is capacity nobody is accounting for; expiry is what returns it.
	MaximumReservationTTL = 24 * time.Hour
)

// ReservationState is the capacity reservation lifecycle.
//
// Held is the only state that expires. Once Bound, the cluster owns the
// workload and the expiry deadline is meaningless -- a bound reservation is
// released by completion or by preemption, never by a clock.
type ReservationState string

const (
	ReservationHeld      ReservationState = "held"
	ReservationBound     ReservationState = "bound"
	ReservationCompleted ReservationState = "completed"
	ReservationReleased  ReservationState = "released"
	ReservationExpired   ReservationState = "expired"
	ReservationPreempted ReservationState = "preempted"
)

func (state ReservationState) Valid() bool {
	switch state {
	case ReservationHeld, ReservationBound, ReservationCompleted,
		ReservationReleased, ReservationExpired, ReservationPreempted:
		return true
	default:
		return false
	}
}

// Terminal reports whether the state releases capacity permanently.
func (state ReservationState) Terminal() bool {
	switch state {
	case ReservationCompleted, ReservationReleased, ReservationExpired, ReservationPreempted:
		return true
	default:
		return false
	}
}

// Reservation is the immutable-versioned capacity hold behind one placement.
//
// Every mutation returns a new value with a re-sealed resourceversion.Version,
// and Validate re-derives that seal from the content. A reservation whose
// digest does not match its fields is rejected wherever it is read, so a
// durable row edited out of band cannot be replayed as authority.
type Reservation struct {
	ID          identifiers.ID          `json:"id"`
	Placement   Placement               `json:"placement"`
	LeaseFence  uint64                  `json:"lease_fence"`
	State       ReservationState        `json:"state"`
	Sequence    uint32                  `json:"sequence"`
	CreatedAt   time.Time               `json:"created_at"`
	ExpiresAt   time.Time               `json:"expires_at"`
	BoundAt     time.Time               `json:"bound_at,omitempty"`
	FinalizedAt time.Time               `json:"finalized_at,omitempty"`
	Assignment  TopologyAssignment      `json:"assignment,omitempty"`
	Preemptor   identifiers.ID          `json:"preemptor,omitempty"`
	Version     resourceversion.Version `json:"resource_version"`
}

func (reservation Reservation) Validate() error {
	if err := validateID(reservation.ID, "reservation", "reservation_id"); err != nil {
		return err
	}
	if err := reservation.Placement.Validate(); err != nil {
		return err
	}
	// A reservation is authority to hold capacity, and authority granted by a
	// process that has lost leadership is exactly what a fencing token exists
	// to reject. Zero is never a valid fence.
	if reservation.LeaseFence == 0 {
		return invalid("lease_fence_invalid", "reservation lease fence is required", nil)
	}
	if !reservation.State.Valid() {
		return invalid("reservation_state_invalid", "reservation state is invalid", nil)
	}
	if reservation.CreatedAt.IsZero() || !reservation.ExpiresAt.After(reservation.CreatedAt) {
		return invalid("reservation_window_invalid", "reservation window is invalid", nil)
	}
	if !reservation.CreatedAt.Equal(reservation.Placement.DecidedAt) {
		return conflict("reservation_placement_time_mismatch", "reservation was not created at its placement decision time")
	}
	ttl := reservation.ExpiresAt.Sub(reservation.CreatedAt)
	if ttl < MinimumReservationTTL || ttl > MaximumReservationTTL {
		return invalid("reservation_ttl_invalid", "reservation TTL is outside bounds", nil)
	}
	if err := reservation.Assignment.Validate(); err != nil {
		return err
	}
	if err := reservation.validateLifecycle(); err != nil {
		return err
	}
	if err := reservation.Version.Validate(); err != nil {
		return invalid("reservation_version_invalid", "reservation version is invalid", err)
	}
	if reservation.Version.Generation() != uint64(reservation.Sequence)+1 {
		return invalid("reservation_version_generation_invalid", "reservation generation does not match its transition count", nil)
	}
	if !reservation.Version.Digest().Equal(reservationDigest(reservation)) {
		return invalid("reservation_version_digest_mismatch", "reservation version does not seal its content", nil)
	}
	return nil
}

// validateLifecycle rejects every timestamp/state combination the transitions
// below cannot produce. It is the check that makes a forged durable row
// unusable rather than merely suspicious.
func (reservation Reservation) validateLifecycle() error {
	bound := !reservation.BoundAt.IsZero()
	finalized := !reservation.FinalizedAt.IsZero()
	if bound && (reservation.BoundAt.Before(reservation.CreatedAt) || !reservation.BoundAt.Before(reservation.ExpiresAt)) {
		return invalid("reservation_bind_time_invalid", "reservation bind time is outside its window", nil)
	}
	if finalized && reservation.FinalizedAt.Before(reservation.CreatedAt) {
		return invalid("reservation_finalized_at_invalid", "reservation finalization time precedes its creation", nil)
	}
	if finalized && bound && reservation.FinalizedAt.Before(reservation.BoundAt) {
		return invalid("reservation_finalized_at_invalid", "reservation finalization time precedes its binding", nil)
	}
	if !reservation.Preemptor.IsZero() && reservation.State != ReservationPreempted {
		return invalid("reservation_preemptor_unexpected", "only a preempted reservation names a preemptor", nil)
	}
	switch reservation.State {
	case ReservationHeld:
		if bound || finalized || !reservation.Assignment.IsZero() || reservation.Sequence != 0 {
			return invalid("reservation_held_lifecycle_invalid", "held reservation has unexpected lifecycle state", nil)
		}
	case ReservationBound:
		if !bound || finalized || reservation.Sequence != 1 {
			return invalid("reservation_bound_lifecycle_invalid", "bound reservation has invalid lifecycle state", nil)
		}
	case ReservationCompleted:
		if !bound || !finalized || reservation.Sequence != 2 {
			return invalid("reservation_completed_lifecycle_invalid", "completed reservation has invalid lifecycle state", nil)
		}
	case ReservationReleased:
		if bound || !finalized || reservation.Sequence != 1 ||
			!reservation.FinalizedAt.Before(reservation.ExpiresAt) || !reservation.Assignment.IsZero() {
			return invalid("reservation_released_lifecycle_invalid", "released reservation has invalid lifecycle state", nil)
		}
	case ReservationExpired:
		if bound || !finalized || reservation.Sequence != 1 ||
			reservation.FinalizedAt.Before(reservation.ExpiresAt) || !reservation.Assignment.IsZero() {
			return invalid("reservation_expired_lifecycle_invalid", "expired reservation has invalid lifecycle state", nil)
		}
	case ReservationPreempted:
		if !finalized || reservation.Preemptor.IsZero() {
			return invalid("reservation_preempted_lifecycle_invalid", "preempted reservation has invalid lifecycle state", nil)
		}
		if err := validateID(reservation.Preemptor, "reservation", "preemptor_id"); err != nil {
			return err
		}
		if reservation.Preemptor == reservation.ID {
			return invalid("reservation_self_preemption", "a reservation cannot preempt itself", nil)
		}
		if bound && reservation.Sequence != 2 || !bound && reservation.Sequence != 1 {
			return invalid("reservation_preempted_lifecycle_invalid", "preempted reservation sequence does not match its binding", nil)
		}
	}
	return nil
}

func (reservation Reservation) clone() Reservation {
	reservation.Placement = reservation.Placement.clone()
	reservation.Assignment = reservation.Assignment.Clone()
	return reservation
}

// Clone returns a deep copy. Repositories return clones so a caller mutating a
// returned map cannot reach into stored state.
func (reservation Reservation) Clone() Reservation { return reservation.clone() }

// NewReservation seals a generation-one hold for a decided placement.
func NewReservation(id identifiers.ID, placement Placement, fence uint64, ttl time.Duration) (Reservation, error) {
	if err := validateID(id, "reservation", "reservation_id"); err != nil {
		return Reservation{}, err
	}
	if err := placement.Validate(); err != nil {
		return Reservation{}, err
	}
	if fence == 0 {
		return Reservation{}, invalid("lease_fence_invalid", "reservation lease fence is required", nil)
	}
	if ttl < MinimumReservationTTL || ttl > MaximumReservationTTL {
		return Reservation{}, invalid("reservation_ttl_invalid", "reservation TTL is outside bounds", nil)
	}
	reservation := Reservation{
		ID:         id,
		Placement:  placement.clone(),
		LeaseFence: fence,
		State:      ReservationHeld,
		CreatedAt:  placement.DecidedAt,
		ExpiresAt:  placement.DecidedAt.Add(ttl),
	}
	sealed, err := versionReservation(reservation, 1)
	if err != nil {
		return Reservation{}, unavailable("reservation_version_unavailable", "reservation version is unavailable", err)
	}
	if err := sealed.Validate(); err != nil {
		return Reservation{}, err
	}
	return sealed, nil
}

// HeldDemand returns the capacity this reservation still charges to the ledger.
// A terminal reservation charges nothing, which is what lets the ledger be
// rebuilt from the reservation set alone.
func (reservation Reservation) HeldDemand() Demand {
	if reservation.State.Terminal() {
		return nil
	}
	return reservation.Placement.Total.Clone()
}

// Active reports whether the reservation still occupies capacity at now.
func (reservation Reservation) Active(now time.Time) bool {
	switch reservation.State {
	case ReservationHeld:
		return now.Before(reservation.ExpiresAt)
	case ReservationBound:
		return true
	default:
		return false
	}
}

// Bind records that the cluster accepted the workload. It is the point after
// which expiry no longer applies, so it must happen inside the held window.
func (reservation Reservation) Bind(at time.Time, assignment TopologyAssignment, fence uint64) (Reservation, error) {
	next, err := reservation.begin(fence)
	if err != nil {
		return Reservation{}, err
	}
	if next.State != ReservationHeld {
		return Reservation{}, conflict("reservation_bind_invalid", "only a held reservation can be bound")
	}
	at = at.Round(0).UTC()
	if at.Before(next.CreatedAt) || !at.Before(next.ExpiresAt) {
		return Reservation{}, failedPrecondition("reservation_expired", "reservation has expired")
	}
	if err := assignment.Validate(); err != nil {
		return Reservation{}, err
	}
	// A recorded assignment that contradicts the sealed constraint means the
	// cluster placed the workload somewhere the decision did not authorize.
	// Accepting it would make the reservation lie about what it is holding.
	if assignment.Constraint != next.Placement.Topology {
		return Reservation{}, conflict("reservation_assignment_mismatch", "topology assignment does not match the placement constraint")
	}
	next.State = ReservationBound
	next.BoundAt = at
	next.Assignment = assignment.Clone()
	return sealReservationTransition(next)
}

// Complete releases capacity after the bound workload finished.
func (reservation Reservation) Complete(at time.Time, fence uint64) (Reservation, error) {
	next, err := reservation.begin(fence)
	if err != nil {
		return Reservation{}, err
	}
	if next.State != ReservationBound {
		return Reservation{}, conflict("reservation_complete_invalid", "only a bound reservation can complete")
	}
	at = at.Round(0).UTC()
	if at.Before(next.BoundAt) {
		return Reservation{}, invalid("reservation_finalized_at_invalid", "completion time precedes the binding", nil)
	}
	next.State = ReservationCompleted
	next.FinalizedAt = at
	return sealReservationTransition(next)
}

// Release returns held capacity before the workload was ever bound.
func (reservation Reservation) Release(at time.Time, fence uint64) (Reservation, error) {
	next, err := reservation.begin(fence)
	if err != nil {
		return Reservation{}, err
	}
	if next.State != ReservationHeld {
		return Reservation{}, conflict("reservation_release_invalid", "only a held reservation can be released")
	}
	at = at.Round(0).UTC()
	if at.Before(next.CreatedAt) {
		return Reservation{}, invalid("reservation_finalized_at_invalid", "release time precedes the reservation", nil)
	}
	if !at.Before(next.ExpiresAt) {
		return Reservation{}, failedPrecondition("reservation_expired", "reservation has expired")
	}
	next.State = ReservationReleased
	next.FinalizedAt = at
	return sealReservationTransition(next)
}

// Expire returns held capacity after the deadline. Calling it early is a failed
// precondition rather than a no-op, because an early expiry would free capacity
// the holder is still entitled to use.
func (reservation Reservation) Expire(at time.Time, fence uint64) (Reservation, error) {
	next, err := reservation.begin(fence)
	if err != nil {
		return Reservation{}, err
	}
	if next.State != ReservationHeld {
		return Reservation{}, conflict("reservation_expire_invalid", "only a held reservation can expire")
	}
	at = at.Round(0).UTC()
	if at.Before(next.ExpiresAt) {
		return Reservation{}, failedPrecondition("reservation_not_expired", "reservation has not expired")
	}
	next.State = ReservationExpired
	next.FinalizedAt = at
	return sealReservationTransition(next)
}

// Preempt records a Go-side eviction. Kueue's preemption is disabled on every
// queue -- reclaimWithinCohort Never, withinClusterQueue Never -- so this is
// the only place a victim is chosen, and the outcome is surfaced as eviction
// and requeue rather than delegated to the cluster.
func (reservation Reservation) Preempt(at time.Time, preemptor identifiers.ID, fence uint64) (Reservation, error) {
	next, err := reservation.begin(fence)
	if err != nil {
		return Reservation{}, err
	}
	if next.State != ReservationHeld && next.State != ReservationBound {
		return Reservation{}, conflict("reservation_preempt_invalid", "only a held or bound reservation can be preempted")
	}
	if err := validateID(preemptor, "reservation", "preemptor_id"); err != nil {
		return Reservation{}, err
	}
	if preemptor == next.ID {
		return Reservation{}, invalid("reservation_self_preemption", "a reservation cannot preempt itself", nil)
	}
	at = at.Round(0).UTC()
	if at.Before(next.CreatedAt) || !next.BoundAt.IsZero() && at.Before(next.BoundAt) {
		return Reservation{}, invalid("reservation_finalized_at_invalid", "preemption time precedes the reservation", nil)
	}
	next.State = ReservationPreempted
	next.FinalizedAt = at
	next.Preemptor = preemptor
	return sealReservationTransition(next)
}

// begin validates the source state and advances the leadership fence. Every
// transition goes through it, so there is no path that writes without both.
func (reservation Reservation) begin(fence uint64) (Reservation, error) {
	if err := reservation.Validate(); err != nil {
		return Reservation{}, err
	}
	if fence == 0 {
		return Reservation{}, invalid("lease_fence_invalid", "reservation lease fence is required", nil)
	}
	if fence < reservation.LeaseFence {
		return Reservation{}, conflict("lease_fence_stale", "writer holds an older leadership fence than the reservation")
	}
	next := reservation.clone()
	next.LeaseFence = fence
	return next, nil
}

func sealReservationTransition(reservation Reservation) (Reservation, error) {
	reservation.Sequence++
	updated, err := versionReservation(reservation, uint64(reservation.Sequence)+1)
	if err != nil {
		return Reservation{}, unavailable("reservation_version_unavailable", "reservation version is unavailable", err)
	}
	if err := updated.Validate(); err != nil {
		return Reservation{}, err
	}
	return updated, nil
}

func versionReservation(reservation Reservation, generation uint64) (Reservation, error) {
	version, err := resourceversion.New(generation, reservationDigest(reservation))
	if err != nil {
		return Reservation{}, err
	}
	reservation.Version = version
	return reservation, nil
}

func reservationDigest(reservation Reservation) identifiers.Digest {
	return identifiers.SHA256String(canonicalJoin(
		"mindclade-capacity-reservation-v1",
		reservation.ID.String(), reservation.Placement.Digest.String(),
		strconv.FormatUint(reservation.LeaseFence, 10), string(reservation.State),
		strconv.FormatUint(uint64(reservation.Sequence), 10),
		reservation.CreatedAt.UTC().Format(time.RFC3339Nano),
		reservation.ExpiresAt.UTC().Format(time.RFC3339Nano),
		reservation.BoundAt.UTC().Format(time.RFC3339Nano),
		reservation.FinalizedAt.UTC().Format(time.RFC3339Nano),
		reservation.Assignment.canonical(), reservation.Preemptor.String(),
	))
}

// SamePlacement compares the caller-owned identity of two reservations. It is
// what makes a retried placement idempotent: the same run, stage, and attempt
// asking for the same capacity is the same reservation, even though a retry
// generates a fresh reservation ID and a fresh decision timestamp.
func (reservation Reservation) SamePlacement(other Reservation) bool {
	left, right := reservation.Placement, other.Placement
	return left.WorkloadID == right.WorkloadID && left.RunID == right.RunID &&
		left.StageID == right.StageID && left.Attempt == right.Attempt &&
		left.Tenant == right.Tenant && left.Workspace == right.Workspace &&
		left.Pool == right.Pool && left.Replicas == right.Replicas &&
		left.PerReplica.Equal(right.PerReplica) && left.Priority == right.Priority &&
		left.Topology == right.Topology
}

// PlacementKey is the idempotency key for a placement attempt.
func PlacementKey(placement Placement) string {
	return canonicalJoin(placement.RunID, placement.StageID, strconv.FormatUint(uint64(placement.Attempt), 10))
}
