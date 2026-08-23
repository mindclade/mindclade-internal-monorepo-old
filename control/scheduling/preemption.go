// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import (
	"sort"
	"time"

	"go.mindclade.dev/libs/go/identifiers"
)

// MaximumPreemptionVictims bounds one plan. A shortfall that would take more
// than this many evictions to cover is not a scheduling decision, it is a
// capacity outage, and draining a queue in one step is worse than reporting it.
const MaximumPreemptionVictims = 256

// PreemptionAction is what the fleet does to a victim.
//
// There is one action, and it is not "delete". Kueue preemption is disabled on
// every ClusterQueue -- reclaimWithinCohort Never, withinClusterQueue Never --
// and no cohort exists to reclaim from, so this package cannot delegate
// reclamation to the cluster. What it can do is evict the workload and requeue
// the work item, which keeps the work visible and lets it be admitted again
// when capacity returns.
type PreemptionAction string

const ActionEvictAndRequeue PreemptionAction = "evict_and_requeue"

// PreemptionRequest asks for room in one capacity domain.
type PreemptionRequest struct {
	Candidate identifiers.ID `json:"candidate"`
	Domain    CapacityDomain `json:"domain"`
	Tenant    string         `json:"tenant"`
	Priority  PriorityClass  `json:"priority"`
	Shortfall Demand         `json:"shortfall"`
}

func (request PreemptionRequest) Validate() error {
	if err := validateID(request.Candidate, "reservation", "candidate_id"); err != nil {
		return err
	}
	if err := request.Domain.Validate(); err != nil {
		return err
	}
	if err := validateName(request.Tenant, "tenant"); err != nil {
		return err
	}
	if !request.Priority.Valid() {
		return invalid("priority_class_invalid", "priority class is not one of the two declared classes", nil)
	}
	return request.Shortfall.ValidateForDomain(request.Domain, true)
}

// Victim is one reservation the plan evicts, and what evicting it returns.
type Victim struct {
	ReservationID identifiers.ID   `json:"reservation_id"`
	Tenant        string           `json:"tenant"`
	Priority      PriorityClass    `json:"priority"`
	Reclaimed     Demand           `json:"reclaimed"`
	Action        PreemptionAction `json:"action"`
}

// PreemptionPlan is an all-or-nothing eviction set. It never covers part of a
// shortfall: a partial eviction destroys work and still does not admit the
// candidate, which is the worst of both outcomes.
type PreemptionPlan struct {
	Candidate identifiers.ID `json:"candidate"`
	Domain    CapacityDomain `json:"domain"`
	Shortfall Demand         `json:"shortfall"`
	Reclaimed Demand         `json:"reclaimed"`
	Victims   []Victim       `json:"victims"`
}

func (plan PreemptionPlan) Validate() error {
	if err := validateID(plan.Candidate, "reservation", "candidate_id"); err != nil {
		return err
	}
	if err := plan.Domain.Validate(); err != nil {
		return err
	}
	if err := plan.Shortfall.ValidateForDomain(plan.Domain, true); err != nil {
		return err
	}
	if len(plan.Victims) == 0 {
		return failedPrecondition("preemption_plan_empty", "preemption plan selects no victims")
	}
	if len(plan.Victims) > MaximumPreemptionVictims {
		return exhausted("preemption_victim_bound", "preemption plan exceeds the victim bound")
	}
	total := Demand(nil)
	seen := make(map[identifiers.ID]struct{}, len(plan.Victims))
	for _, victim := range plan.Victims {
		if err := validateID(victim.ReservationID, "reservation", "victim_id"); err != nil {
			return err
		}
		if victim.ReservationID == plan.Candidate {
			return invalid("preemption_self_victim", "a candidate cannot be its own victim", nil)
		}
		if _, exists := seen[victim.ReservationID]; exists {
			return invalid("preemption_duplicate_victim", "preemption plan repeats a victim", nil)
		}
		seen[victim.ReservationID] = struct{}{}
		if victim.Action != ActionEvictAndRequeue {
			return invalid("preemption_action_invalid", "preemption action is invalid", nil)
		}
		if err := victim.Reclaimed.ValidateForDomain(plan.Domain, true); err != nil {
			return err
		}
		var err error
		total, err = total.add(victim.Reclaimed)
		if err != nil {
			return err
		}
	}
	if !total.Equal(plan.Reclaimed) {
		return conflict("preemption_reclaim_mismatch", "preemption plan total does not match its victims")
	}
	if !plan.Shortfall.Fits(plan.Reclaimed) {
		return failedPrecondition("preemption_plan_insufficient", "preemption plan does not cover the shortfall")
	}
	return nil
}

// SelectVictims chooses the eviction set for one shortfall.
//
// The rules are the cluster's, not preferences:
//
//   - Batch may never preempt. mindclade-batch declares preemptionPolicy Never,
//     so a plan built for a batch candidate would evict real work and then fail
//     to admit anything.
//   - Victims must be strictly lower priority. Kubernetes preempts lower
//     priority only, and equal-priority eviction would let two workloads evict
//     each other indefinitely.
//   - Victims must share the candidate's capacity domain. There are no cohorts,
//     so capacity freed in one ClusterQueue is not available to another; a
//     cross-domain victim is destroyed for nothing.
//
// Ordering is fully deterministic so two replicas evaluating the same fleet
// choose the same victims: lowest priority first, then the most over-served
// tenant, then the most recently started work, then reservation ID. Newest
// first is deliberate -- evicting the job that has been running for six hours
// throws away six hours.
func SelectVictims(request PreemptionRequest, held []Reservation, share FairShare, now time.Time) (PreemptionPlan, error) {
	if err := request.Validate(); err != nil {
		return PreemptionPlan{}, err
	}
	if now.IsZero() {
		return PreemptionPlan{}, invalid("decision_time_invalid", "preemption decision time is required", nil)
	}
	if !request.Priority.MayPreempt() {
		return PreemptionPlan{}, denied("batch_may_not_preempt", "this priority class declares preemptionPolicy Never and may not preempt")
	}
	if err := share.Validate(); err != nil {
		return PreemptionPlan{}, err
	}
	if share.Domain != request.Domain {
		return PreemptionPlan{}, conflict("preemption_share_domain_mismatch", "fair-share view is for a different capacity domain")
	}
	if len(held) > MaximumSnapshotDomains*MaximumShareClaims {
		return PreemptionPlan{}, exhausted("preemption_candidate_bound", "held reservation set exceeds its bound")
	}

	type ranked struct {
		reservation Reservation
		position    uint64
		started     time.Time
	}
	eligible := make([]ranked, 0, len(held))
	for _, reservation := range held {
		if err := reservation.Validate(); err != nil {
			return PreemptionPlan{}, err
		}
		if reservation.ID == request.Candidate || !reservation.Active(now) {
			continue
		}
		if reservation.Placement.Pool.Domain != request.Domain {
			continue
		}
		if !request.Priority.Preempts(reservation.Placement.Priority) {
			continue
		}
		position, err := share.Position(reservation.Placement.Tenant)
		if err != nil {
			return PreemptionPlan{}, err
		}
		started := reservation.CreatedAt
		if !reservation.BoundAt.IsZero() {
			started = reservation.BoundAt
		}
		eligible = append(eligible, ranked{reservation: reservation, position: position, started: started})
	}
	sort.Slice(eligible, func(left, right int) bool {
		leftEntry, rightEntry := eligible[left], eligible[right]
		leftValue, rightValue := leftEntry.reservation.Placement.Priority.Value(), rightEntry.reservation.Placement.Priority.Value()
		if leftValue != rightValue {
			return leftValue < rightValue
		}
		if leftEntry.position != rightEntry.position {
			return leftEntry.position > rightEntry.position
		}
		if !leftEntry.started.Equal(rightEntry.started) {
			return leftEntry.started.After(rightEntry.started)
		}
		return leftEntry.reservation.ID.String() < rightEntry.reservation.ID.String()
	})

	reclaimed := Demand(nil)
	victims := make([]Victim, 0, len(eligible))
	for _, entry := range eligible {
		if request.Shortfall.Fits(reclaimed) {
			break
		}
		if len(victims) == MaximumPreemptionVictims {
			return PreemptionPlan{}, exhausted("preemption_victim_bound", "covering the shortfall would exceed the victim bound")
		}
		held := entry.reservation.HeldDemand()
		if held.IsZero() {
			continue
		}
		next, err := reclaimed.add(held)
		if err != nil {
			return PreemptionPlan{}, err
		}
		reclaimed = next
		victims = append(victims, Victim{
			ReservationID: entry.reservation.ID,
			Tenant:        entry.reservation.Placement.Tenant,
			Priority:      entry.reservation.Placement.Priority,
			Reclaimed:     held,
			Action:        ActionEvictAndRequeue,
		})
	}
	if !request.Shortfall.Fits(reclaimed) {
		// All-or-nothing. Returning the partial set would let a caller evict
		// work that still does not make room for the candidate.
		return PreemptionPlan{}, exhausted("preemption_insufficient", "no eligible victim set covers the shortfall")
	}
	plan := PreemptionPlan{
		Candidate: request.Candidate,
		Domain:    request.Domain,
		Shortfall: request.Shortfall.Clone(),
		Reclaimed: reclaimed,
		Victims:   victims,
	}
	if err := plan.Validate(); err != nil {
		return PreemptionPlan{}, err
	}
	return plan, nil
}

// ApplyPreemption transitions every victim in a plan. It is all-or-nothing: a
// single failing transition abandons the whole set, so a caller never persists
// half an eviction.
func ApplyPreemption(plan PreemptionPlan, held []Reservation, at time.Time, fence uint64) ([]Reservation, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if at.IsZero() {
		return nil, invalid("decision_time_invalid", "preemption time is required", nil)
	}
	byID := make(map[identifiers.ID]Reservation, len(held))
	for _, reservation := range held {
		byID[reservation.ID] = reservation
	}
	evicted := make([]Reservation, 0, len(plan.Victims))
	for _, victim := range plan.Victims {
		reservation, exists := byID[victim.ReservationID]
		if !exists {
			return nil, notFound("preemption_victim_not_found", "preemption victim is not in the held set")
		}
		if !reservation.HeldDemand().Equal(victim.Reclaimed) {
			return nil, conflict("preemption_victim_changed", "preemption victim no longer holds the planned demand")
		}
		updated, err := reservation.Preempt(at, plan.Candidate, fence)
		if err != nil {
			return nil, err
		}
		evicted = append(evicted, updated)
	}
	return evicted, nil
}
