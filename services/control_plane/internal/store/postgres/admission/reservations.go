// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admissionpostgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

func (store *Store) Reserve(ctx context.Context, snapshot admission.PolicySnapshot, candidate admission.Reservation, now time.Time) (admission.Reservation, bool, error) {
	const operation = "admission.postgres.Reserve"
	if err := snapshot.Validate(); err != nil {
		return admission.Reservation{}, false, err
	}
	if err := candidate.Validate(); err != nil {
		return admission.Reservation{}, false, err
	}
	if _, err := sqlUint(ctx, candidate.PolicyEpoch, "policy_epoch"); err != nil {
		return admission.Reservation{}, false, err
	}
	result, err := runMutation(ctx, store, operation, func(txContext context.Context) (reserveResult, error) {
		if existing, found, lookupErr := store.lookupIdempotency(txContext, candidate); lookupErr != nil {
			return reserveResult{}, lookupErr
		} else if found {
			decisionNow := store.clock.Now().Round(0).UTC()
			if !existing.SameAdmissionRequest(candidate) {
				return reserveResult{}, domainError(txContext, faults.CodeConflict, "idempotency_payload_mismatch", "idempotency key was reused for a different request", operation)
			}
			if existing.State == admission.ReservationReserved && !decisionNow.Before(existing.ExpiresAt) {
				expired, transitionErr := existing.Expire(decisionNow)
				if transitionErr != nil {
					return reserveResult{}, transitionErr
				}
				if persistErr := store.updateReservation(txContext, existing.Version, expired, decisionNow); persistErr != nil {
					return reserveResult{}, persistErr
				}
				if emitErr := store.emitReservation(txContext, "ai_gateway.reservation.expire", expired); emitErr != nil {
					return reserveResult{}, emitErr
				}
				existing = expired
			}
			if existing.State == admission.ReservationExpired {
				return reserveResult{semantic: domainError(txContext, faults.CodeFailedPrecondition, "reservation_expired", "idempotent reservation has expired", operation)}, nil
			}
			return reserveResult{reservation: existing, replayed: true}, nil
		}

		current, lockErr := store.lockPolicy(txContext, candidate.Subject, candidate.Workspace)
		if lockErr != nil {
			return reserveResult{}, lockErr
		}
		if current.Entitlement.ID.String() != snapshot.Entitlement.ID.String() ||
			current.Entitlement.Version.String() != snapshot.Entitlement.Version.String() ||
			current.Budget.ID.String() != snapshot.Budget.ID.String() ||
			current.Budget.Version.String() != snapshot.Budget.Version.String() {
			return reserveResult{}, domainError(txContext, faults.CodeConflict, "policy_snapshot_stale", "policy snapshot is stale", operation)
		}
		if validationErr := candidate.ValidateInitial(current, now); validationErr != nil {
			return reserveResult{}, validationErr
		}
		decisionNow := store.clock.Now().Round(0).UTC()
		if decisionNow.Before(candidate.CreatedAt) {
			return reserveResult{}, internal(txContext, errors.New("admission clock moved before candidate creation"), operation, "admission_clock_regressed")
		}
		if !decisionNow.Before(candidate.ExpiresAt) {
			return reserveResult{}, domainError(txContext, faults.CodeFailedPrecondition, "reservation_window_elapsed", "reservation window elapsed before persistence", operation)
		}
		if !current.Entitlement.ActiveAt(decisionNow) || !current.Budget.ActiveAt(decisionNow) {
			return reserveResult{}, domainError(txContext, faults.CodeFailedPrecondition, "policy_window_inactive", "policy window is inactive", operation)
		}
		if current.Entitlement.PolicyEpoch != candidate.PolicyEpoch ||
			!current.Entitlement.Allows(candidate.Route) || !candidate.Reserved.Fits(current.Entitlement.MaximumRequest) {
			return reserveResult{}, domainError(txContext, faults.CodePermissionDenied, "entitlement_changed", "entitlement no longer allows the request", operation)
		}
		used, usageErr := store.usedQuota(txContext, current.Budget.ID, decisionNow)
		if usageErr != nil {
			return reserveResult{}, usageErr
		}
		if !canAdd(used, candidate.Reserved, current.Budget.Limit) {
			return reserveResult{}, domainError(txContext, faults.CodeResourceExhausted, "budget_exhausted", "budget cannot satisfy the reservation", operation)
		}
		inserted, insertErr := store.insertReservation(txContext, candidate, decisionNow)
		if insertErr != nil {
			return reserveResult{}, insertErr
		}
		if !inserted {
			return reserveResult{}, faults.New(faults.CodeAborted, "concurrent idempotent reservation must be retried",
				faults.WithReason("admission_idempotency_race"), faults.WithOperation(operation),
				faults.WithRetryPolicy(faults.ImmediateRetry(3)), faults.WithContextMetadata(txContext))
		}
		if emitErr := store.emitReservation(txContext, "ai_gateway.reservation.reserve", candidate); emitErr != nil {
			return reserveResult{}, emitErr
		}
		return reserveResult{reservation: candidate}, nil
	})
	if err != nil {
		return admission.Reservation{}, false, err
	}
	if result.semantic != nil {
		return admission.Reservation{}, false, result.semantic
	}
	return result.reservation, result.replayed, nil
}

type reserveResult struct {
	reservation admission.Reservation
	replayed    bool
	semantic    error
}

func (store *Store) lookupIdempotency(ctx context.Context, candidate admission.Reservation) (admission.Reservation, bool, error) {
	var document []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE idempotency_scope=$1 AND idempotency_key=$2 FOR UPDATE`, store.reservations),
		candidate.Idempotency.Scope.String(), candidate.Idempotency.Key.String()).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return admission.Reservation{}, false, nil
	}
	if err != nil {
		return admission.Reservation{}, false, provider(ctx, err, "admission.postgres.Reserve.LookupIdempotency")
	}
	reservation, err := decodeDocument[admission.Reservation](ctx, document, "admission.postgres.Reserve.LookupIdempotency")
	return reservation, true, err
}

func (store *Store) lockPolicy(ctx context.Context, subject, workspace string) (admission.PolicySnapshot, error) {
	var entitlementDocument []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE subject=$1 AND workspace=$2 FOR UPDATE`, store.entitlements),
		subject, workspace).Scan(&entitlementDocument)
	if errors.Is(err, sql.ErrNoRows) {
		return admission.PolicySnapshot{}, domainError(ctx, faults.CodeConflict, "policy_snapshot_stale", "policy snapshot is stale", "admission.postgres.Reserve")
	}
	if err != nil {
		return admission.PolicySnapshot{}, provider(ctx, err, "admission.postgres.Reserve.LockEntitlement")
	}
	entitlement, err := decodeDocument[admission.Entitlement](ctx, entitlementDocument, "admission.postgres.Reserve.LockEntitlement")
	if err != nil {
		return admission.PolicySnapshot{}, err
	}
	var budgetDocument []byte
	err = store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE workspace=$1 FOR UPDATE`, store.budgets), workspace).Scan(&budgetDocument)
	if errors.Is(err, sql.ErrNoRows) {
		return admission.PolicySnapshot{}, domainError(ctx, faults.CodeConflict, "policy_snapshot_stale", "policy snapshot is stale", "admission.postgres.Reserve")
	}
	if err != nil {
		return admission.PolicySnapshot{}, provider(ctx, err, "admission.postgres.Reserve.LockBudget")
	}
	budget, err := decodeDocument[admission.Budget](ctx, budgetDocument, "admission.postgres.Reserve.LockBudget")
	if err != nil {
		return admission.PolicySnapshot{}, err
	}
	snapshot := admission.PolicySnapshot{Entitlement: entitlement, Budget: budget}
	if err := snapshot.Validate(); err != nil {
		return admission.PolicySnapshot{}, internal(ctx, err, "admission.postgres.Reserve", "admission_policy_snapshot_invalid")
	}
	return snapshot, nil
}

func (store *Store) usedQuota(ctx context.Context, budgetID identifiers.ID, now time.Time) (admission.Quota, error) {
	query := fmt.Sprintf(`SELECT
COALESCE(SUM(CASE WHEN state='committed' THEN actual_requests ELSE reserved_requests END),0),
COALESCE(SUM(CASE WHEN state='committed' THEN actual_input_tokens ELSE reserved_input_tokens END),0),
COALESCE(SUM(CASE WHEN state='committed' THEN actual_output_tokens ELSE reserved_output_tokens END),0),
COALESCE(SUM(CASE WHEN state='committed' THEN actual_cost_micros ELSE reserved_cost_micros END),0)
FROM %s WHERE budget_id=$1 AND (state IN ('committed','dispatched','reconciliation_pending') OR (state='reserved' AND expires_at>$2))`, store.reservations)
	var requests, inputTokens, outputTokens, costMicros int64
	if err := store.executor(ctx).QueryRowContext(ctx, query, budgetID.String(), now).Scan(
		&requests, &inputTokens, &outputTokens, &costMicros); err != nil {
		return nil, provider(ctx, err, "admission.postgres.Reserve.SumBudget")
	}
	if requests < 0 || inputTokens < 0 || outputTokens < 0 || costMicros < 0 {
		return nil, internal(ctx, errors.New("negative quota aggregate"), "admission.postgres.Reserve.SumBudget", "admission_budget_ledger_corrupt")
	}
	return admission.Quota{
		admission.UnitRequests: uint64(requests), admission.UnitInputTokens: uint64(inputTokens),
		admission.UnitOutputTokens: uint64(outputTokens), admission.UnitCostMicros: uint64(costMicros),
	}, nil
}

func canAdd(used, requested, limit admission.Quota) bool {
	for _, unit := range []admission.Unit{admission.UnitRequests, admission.UnitInputTokens, admission.UnitOutputTokens, admission.UnitCostMicros} {
		if requested[unit] > limit[unit] || used[unit] > limit[unit]-requested[unit] {
			return false
		}
	}
	return true
}

func (store *Store) insertReservation(ctx context.Context, reservation admission.Reservation, now time.Time) (bool, error) {
	document, err := marshalDocument(ctx, reservation, "admission.postgres.Reserve")
	if err != nil {
		return false, err
	}
	generation, err := sqlUint(ctx, reservation.Version.Generation(), "resource_generation")
	if err != nil {
		return false, err
	}
	query := fmt.Sprintf(`INSERT INTO %s (
reservation_id,idempotency_scope,idempotency_key,request_digest,subject,workspace,budget_id,state,
reserved_requests,reserved_input_tokens,reserved_output_tokens,reserved_cost_micros,
actual_requests,actual_input_tokens,actual_output_tokens,actual_cost_micros,
created_at,expires_at,finalized_at,resource_version,resource_generation,document,written_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,0,0,0,0,$13,$14,NULL,$15,$16,$17::jsonb,$18)
ON CONFLICT (idempotency_scope,idempotency_key) DO NOTHING`, store.reservations)
	result, err := store.executor(ctx).ExecContext(ctx, query,
		reservation.ID.String(), reservation.Idempotency.Scope.String(), reservation.Idempotency.Key.String(),
		reservation.RequestDigest.String(), reservation.Subject, reservation.Workspace, reservation.BudgetID.String(),
		string(reservation.State), int64(reservation.Reserved[admission.UnitRequests]),
		int64(reservation.Reserved[admission.UnitInputTokens]), int64(reservation.Reserved[admission.UnitOutputTokens]),
		int64(reservation.Reserved[admission.UnitCostMicros]), reservation.CreatedAt, reservation.ExpiresAt,
		reservation.Version.String(), generation, document, now)
	if err != nil {
		return false, provider(ctx, err, "admission.postgres.Reserve.Insert")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, provider(ctx, err, "admission.postgres.Reserve.Insert.RowsAffected")
	}
	if affected < 0 || affected > 1 {
		return false, internal(ctx, fmt.Errorf("reservation insert affected %d rows", affected), "admission.postgres.Reserve.Insert", "admission_insert_cardinality_invalid")
	}
	return affected == 1, nil
}

func (store *Store) Commit(ctx context.Context, id identifiers.ID, expected resourceversion.Version, digest identifiers.Digest, subject string, actual admission.Quota, now time.Time) (admission.Reservation, bool, error) {
	return store.finalize(ctx, id, expected, digest, subject, actual, admission.ReservationCommitted, now)
}

func (store *Store) Dispatch(ctx context.Context, id identifiers.ID, expected resourceversion.Version, digest identifiers.Digest, subject string, now time.Time) (admission.Reservation, bool, error) {
	return store.finalize(ctx, id, expected, digest, subject, nil, admission.ReservationDispatched, now)
}

func (store *Store) MarkReconciliationPending(ctx context.Context, id identifiers.ID, expected resourceversion.Version, digest identifiers.Digest, subject string, now time.Time) (admission.Reservation, bool, error) {
	return store.finalize(ctx, id, expected, digest, subject, nil, admission.ReservationReconciliationPending, now)
}

func (store *Store) Reconcile(ctx context.Context, id identifiers.ID, expected resourceversion.Version, digest identifiers.Digest, subject string, now time.Time) (admission.Reservation, bool, error) {
	return store.finalize(ctx, id, expected, digest, subject, nil, admission.ReservationCommitted, now)
}

func (store *Store) Release(ctx context.Context, id identifiers.ID, expected resourceversion.Version, digest identifiers.Digest, subject string, now time.Time) (admission.Reservation, bool, error) {
	return store.finalize(ctx, id, expected, digest, subject, nil, admission.ReservationReleased, now)
}

type finalizationResult struct {
	reservation admission.Reservation
	replayed    bool
	semantic    error
}

func (store *Store) finalize(ctx context.Context, id identifiers.ID, expected resourceversion.Version, digest identifiers.Digest, subject string, actual admission.Quota, target admission.ReservationState, _ time.Time) (admission.Reservation, bool, error) {
	const operation = "admission.postgres.Finalize"
	if err := id.Validate(); err != nil || id.Kind().String() != "reservation" || expected.Validate() != nil || !digest.Valid() {
		return admission.Reservation{}, false, domainError(ctx, faults.CodeInvalidArgument, "finalization_identity_invalid", "finalization identity is invalid", operation)
	}
	result, err := runMutation(ctx, store, operation, func(txContext context.Context) (finalizationResult, error) {
		current, loadErr := store.lockReservation(txContext, id)
		if loadErr != nil {
			return finalizationResult{}, loadErr
		}
		decisionNow := store.clock.Now().Round(0).UTC()
		if !current.RequestDigest.Equal(digest) {
			return finalizationResult{}, domainError(txContext, faults.CodePermissionDenied, "request_digest_mismatch", "request digest does not own reservation", operation)
		}
		if current.Subject != subject {
			return finalizationResult{}, domainError(txContext, faults.CodePermissionDenied, "reservation_subject_mismatch", "subject does not own reservation", operation)
		}
		if current.State == target && (target != admission.ReservationCommitted || len(actual) == 0 && current.Actual.Equal(current.Reserved) || current.Actual.Equal(actual)) {
			return finalizationResult{reservation: current, replayed: true}, nil
		}
		if current.State == admission.ReservationExpired {
			return finalizationResult{semantic: domainError(txContext, faults.CodeFailedPrecondition, "reservation_expired", "reservation has expired", operation)}, nil
		}
		if current.Version.String() != expected.String() {
			return finalizationResult{}, domainError(txContext, faults.CodeConflict, "reservation_version_stale", "reservation version is stale", operation)
		}
		if (target == admission.ReservationDispatched || target == admission.ReservationReleased) && !decisionNow.Before(current.ExpiresAt) {
			expired, transitionErr := current.Expire(decisionNow)
			if transitionErr != nil {
				return finalizationResult{}, transitionErr
			}
			if persistErr := store.updateReservation(txContext, current.Version, expired, decisionNow); persistErr != nil {
				return finalizationResult{}, persistErr
			}
			if emitErr := store.emitReservation(txContext, "ai_gateway.reservation.expire", expired); emitErr != nil {
				return finalizationResult{}, emitErr
			}
			return finalizationResult{semantic: domainError(txContext, faults.CodeFailedPrecondition, "reservation_expired", "reservation has expired", operation)}, nil
		}
		var updated admission.Reservation
		var transitionErr error
		switch target {
		case admission.ReservationDispatched:
			updated, transitionErr = current.Dispatch(decisionNow)
		case admission.ReservationReconciliationPending:
			updated, transitionErr = current.MarkReconciliationPending(decisionNow)
		case admission.ReservationCommitted:
			if len(actual) == 0 {
				updated, transitionErr = current.Reconcile(decisionNow)
			} else {
				updated, transitionErr = current.Commit(actual, decisionNow)
			}
		case admission.ReservationReleased:
			updated, transitionErr = current.Release(decisionNow)
		default:
			transitionErr = domainError(txContext, faults.CodeInvalidArgument, "reservation_transition_invalid", "reservation transition is invalid", operation)
		}
		if transitionErr != nil {
			return finalizationResult{}, transitionErr
		}
		if persistErr := store.updateReservation(txContext, current.Version, updated, decisionNow); persistErr != nil {
			return finalizationResult{}, persistErr
		}
		action := map[admission.ReservationState]string{
			admission.ReservationDispatched:            "ai_gateway.reservation.dispatch",
			admission.ReservationReconciliationPending: "ai_gateway.reservation.reconciliation_pending",
			admission.ReservationCommitted:             "ai_gateway.reservation.commit",
			admission.ReservationReleased:              "ai_gateway.reservation.release",
		}[target]
		if len(actual) == 0 && target == admission.ReservationCommitted {
			action = "ai_gateway.reservation.reconcile"
		}
		if emitErr := store.emitReservation(txContext, action, updated); emitErr != nil {
			return finalizationResult{}, emitErr
		}
		return finalizationResult{reservation: updated}, nil
	})
	if err != nil {
		return admission.Reservation{}, false, err
	}
	if result.semantic != nil {
		return admission.Reservation{}, false, result.semantic
	}
	return result.reservation, result.replayed, nil
}

func (store *Store) lockReservation(ctx context.Context, id identifiers.ID) (admission.Reservation, error) {
	var document []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE reservation_id=$1 FOR UPDATE`, store.reservations),
		id.String()).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return admission.Reservation{}, domainError(ctx, faults.CodeNotFound, "reservation_not_found", "reservation was not found", "admission.postgres.Finalize")
	}
	if err != nil {
		return admission.Reservation{}, provider(ctx, err, "admission.postgres.Finalize.Lock")
	}
	return decodeDocument[admission.Reservation](ctx, document, "admission.postgres.Finalize.Lock")
}

func (store *Store) updateReservation(ctx context.Context, expected resourceversion.Version, reservation admission.Reservation, now time.Time) error {
	document, err := marshalDocument(ctx, reservation, "admission.postgres.UpdateReservation")
	if err != nil {
		return err
	}
	generation, err := sqlUint(ctx, reservation.Version.Generation(), "resource_generation")
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`UPDATE %s SET state=$2,
actual_requests=$3,actual_input_tokens=$4,actual_output_tokens=$5,actual_cost_micros=$6,
finalized_at=$7,resource_version=$8,resource_generation=$9,document=$10::jsonb,written_at=$11
WHERE reservation_id=$1 AND resource_version=$12`, store.reservations)
	finalizedAt := sql.NullTime{Time: reservation.FinalizedAt, Valid: !reservation.FinalizedAt.IsZero()}
	result, err := store.executor(ctx).ExecContext(ctx, query,
		reservation.ID.String(), string(reservation.State), int64(reservation.Actual[admission.UnitRequests]),
		int64(reservation.Actual[admission.UnitInputTokens]), int64(reservation.Actual[admission.UnitOutputTokens]),
		int64(reservation.Actual[admission.UnitCostMicros]), finalizedAt, reservation.Version.String(),
		generation, document, now, expected.String())
	if err != nil {
		return provider(ctx, err, "admission.postgres.UpdateReservation")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return provider(ctx, err, "admission.postgres.UpdateReservation.RowsAffected")
	}
	if affected != 1 {
		return domainError(ctx, faults.CodeConflict, "reservation_version_stale", "reservation version is stale", "admission.postgres.UpdateReservation")
	}
	return nil
}

func (store *Store) Get(ctx context.Context, id identifiers.ID) (admission.Reservation, error) {
	const operation = "admission.postgres.Get"
	if err := store.validate(ctx, operation); err != nil {
		return admission.Reservation{}, err
	}
	if err := id.Validate(); err != nil || id.Kind().String() != "reservation" {
		return admission.Reservation{}, domainError(ctx, faults.CodeInvalidArgument, "reservation_id_invalid", "reservation ID is invalid", operation)
	}
	var document []byte
	err := store.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE reservation_id=$1`, store.reservations), id.String()).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return admission.Reservation{}, domainError(ctx, faults.CodeNotFound, "reservation_not_found", "reservation was not found", operation)
	}
	if err != nil {
		return admission.Reservation{}, provider(ctx, err, operation)
	}
	return decodeDocument[admission.Reservation](ctx, document, operation)
}

// ExpireReservations materializes overdue terminal transitions in bounded,
// skip-locked batches so multiple maintenance workers can sweep safely.
func (store *Store) ExpireReservations(ctx context.Context, limit int, _ time.Time) ([]admission.Reservation, error) {
	const operation = "admission.postgres.ExpireReservations"
	if limit <= 0 || limit > 1000 {
		return nil, domainError(ctx, faults.CodeInvalidArgument, "expiration_limit_invalid", "expiration limit is outside bounds", operation)
	}
	return runMutation(ctx, store, operation, func(txContext context.Context) ([]admission.Reservation, error) {
		decisionNow := store.clock.Now().Round(0).UTC()
		rows, err := store.executor(txContext).QueryContext(txContext,
			fmt.Sprintf(`SELECT document FROM %s WHERE ((state IN ('reserved','dispatched') AND expires_at<=$1) OR state='reconciliation_pending') ORDER BY expires_at,reservation_id FOR UPDATE SKIP LOCKED LIMIT $2`, store.reservations),
			decisionNow, limit)
		if err != nil {
			return nil, provider(txContext, err, operation)
		}
		defer rows.Close()
		current := make([]admission.Reservation, 0, limit)
		for rows.Next() {
			var document []byte
			if scanErr := rows.Scan(&document); scanErr != nil {
				return nil, provider(txContext, scanErr, operation+".Scan")
			}
			reservation, decodeErr := decodeDocument[admission.Reservation](txContext, document, operation)
			if decodeErr != nil {
				return nil, decodeErr
			}
			current = append(current, reservation)
		}
		if err := rows.Err(); err != nil {
			return nil, provider(txContext, err, operation+".Rows")
		}
		transitioned := make([]admission.Reservation, 0, len(current))
		for _, reservation := range current {
			var updated admission.Reservation
			var transitionErr error
			action := "ai_gateway.reservation.expire"
			switch reservation.State {
			case admission.ReservationReserved:
				updated, transitionErr = reservation.Expire(decisionNow)
			case admission.ReservationDispatched:
				updated, transitionErr = reservation.MarkReconciliationPending(decisionNow)
				action = "ai_gateway.reservation.reconciliation_pending"
			case admission.ReservationReconciliationPending:
				updated, transitionErr = reservation.Reconcile(decisionNow)
				action = "ai_gateway.reservation.reconcile"
			}
			if transitionErr != nil {
				return nil, transitionErr
			}
			if persistErr := store.updateReservation(txContext, reservation.Version, updated, decisionNow); persistErr != nil {
				return nil, persistErr
			}
			if emitErr := store.emitReservation(txContext, action, updated); emitErr != nil {
				return nil, emitErr
			}
			transitioned = append(transitioned, updated)
		}
		return transitioned, nil
	})
}
