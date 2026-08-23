// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package schedulingpostgres

import (
	"context"
	"time"

	"go.mindclade.dev/control/scheduling"
	"go.mindclade.dev/libs/go/faults"
)

// Preempt applies one eviction set, all or nothing.
//
// The plan is all-or-nothing in the domain and all-or-nothing here: every
// victim is transitioned inside one transaction, so a crash can never leave a
// fleet where half a plan was executed. A partial eviction destroys work and
// still does not admit the candidate, which is the worst of both outcomes.
//
// Victims are locked in the plan's own order. That would be a deadlock hazard
// against a concurrent Preempt with an overlapping, differently ordered victim
// set -- except that both transactions took the singleton ledger row first, so
// they are already serialized before either reaches a victim. It is the clearest
// payoff of locking one hot row first: the lock ordering problem disappears
// instead of being solved per query.
func (store *Store) Preempt(
	ctx context.Context, plan scheduling.PreemptionPlan, fence uint64, now time.Time,
) ([]scheduling.Reservation, bool, error) {
	const operation = "scheduling.postgres.Preempt"
	if err := store.validate(ctx, operation); err != nil {
		return nil, false, err
	}
	if err := plan.Validate(); err != nil {
		return nil, false, err
	}
	if now.IsZero() {
		return nil, false, domainError(ctx, faults.CodeInvalidArgument,
			"snapshot_time_invalid", "preemption time is required", operation)
	}
	transactionTime := now.Round(0).UTC()

	result, err := runMutation(ctx, store, operation, func(txContext context.Context) (preemptionResult, error) {
		state, lockErr := store.lockLedger(txContext, operation)
		if lockErr != nil {
			return preemptionResult{}, lockErr
		}
		if expireErr := store.expire(txContext, transactionTime, operation); expireErr != nil {
			return preemptionResult{}, expireErr
		}
		if fence == 0 {
			return preemptionResult{}, domainError(txContext, faults.CodeInvalidArgument,
				"lease_fence_invalid", "leadership fence is required", operation)
		}
		if fence < state.fence {
			return preemptionResult{}, domainError(txContext, faults.CodeConflict,
				"lease_fence_stale", "writer holds an older leadership fence than the store", operation)
		}

		// A plan whose victims are already preempted by this candidate is a
		// replay, not a conflict: the worker crashed between the write and the
		// ack. Every victim has to match, so a plan that landed only in part
		// -- which this store makes unreachable, but a hand-edited row could
		// still produce -- is re-applied rather than acknowledged.
		replayed := true
		held := make([]scheduling.Reservation, 0, len(plan.Victims))
		for _, victim := range plan.Victims {
			reservation, found, lookupErr := store.lockReservation(txContext,
				`reservation_id=$1`, victim.ReservationID.String(), operation)
			if lookupErr != nil {
				return preemptionResult{}, lookupErr
			}
			if !found {
				return preemptionResult{}, domainError(txContext, faults.CodeNotFound,
					"preemption_victim_not_found", "preemption victim was not found", operation)
			}
			if reservation.State != scheduling.ReservationPreempted || reservation.Preemptor != plan.Candidate {
				replayed = false
			}
			held = append(held, reservation)
		}
		if replayed {
			return preemptionResult{reservations: held, replayed: true}, nil
		}

		// ApplyPreemption is the domain's, not a loop here. It re-checks that
		// every victim still holds exactly the demand the plan was built from
		// and seals each transition through Reservation.Preempt, so an eviction
		// cannot land on a reservation that moved since the plan was chosen.
		evicted, applyErr := scheduling.ApplyPreemption(plan, held, transactionTime, fence)
		if applyErr != nil {
			return preemptionResult{}, applyErr
		}
		for _, reservation := range evicted {
			if writeErr := store.updateReservation(txContext, reservation, transactionTime, operation); writeErr != nil {
				return preemptionResult{}, writeErr
			}
			if emitErr := store.emitReservation(txContext, callerAuthored, reservation); emitErr != nil {
				return preemptionResult{}, emitErr
			}
		}
		if ledgerErr := store.advanceLedger(txContext, fence, transactionTime, operation); ledgerErr != nil {
			return preemptionResult{}, ledgerErr
		}
		return preemptionResult{reservations: evicted}, nil
	})
	if err != nil {
		return nil, false, err
	}
	return result.reservations, result.replayed, nil
}

type preemptionResult struct {
	reservations []scheduling.Reservation
	replayed     bool
}
