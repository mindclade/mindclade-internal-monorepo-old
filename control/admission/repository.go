// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admission

import (
	"context"
	"sync"
	"time"

	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

// PolicySnapshot binds the exact entitlement and budget versions checked by a reservation.
type PolicySnapshot struct {
	Entitlement Entitlement
	Budget      Budget
}

func (snapshot PolicySnapshot) Validate() error {
	if err := snapshot.Entitlement.Validate(); err != nil {
		return err
	}
	if err := snapshot.Budget.Validate(); err != nil {
		return err
	}
	if snapshot.Entitlement.Workspace != snapshot.Budget.Workspace {
		return invalid("policy_workspace_mismatch", "entitlement and budget workspaces differ", nil)
	}
	return nil
}

func (snapshot PolicySnapshot) clone() PolicySnapshot {
	return PolicySnapshot{Entitlement: snapshot.Entitlement.clone(), Budget: snapshot.Budget.clone()}
}

// Repository implementations must make Reserve/Commit/Release atomic with their audit and
// outbox writes. Reserve must compare both policy versions in the same transaction as quota use.
type Repository interface {
	Snapshot(context.Context, string, string) (PolicySnapshot, error)
	Reserve(context.Context, PolicySnapshot, Reservation, time.Time) (Reservation, bool, error)
	Commit(context.Context, identifiers.ID, resourceversion.Version, identifiers.Digest, string, Quota, time.Time) (Reservation, bool, error)
	Release(context.Context, identifiers.ID, resourceversion.Version, identifiers.Digest, string, time.Time) (Reservation, bool, error)
	Get(context.Context, identifiers.ID) (Reservation, error)
}

// MemoryRepository is a bounded, concurrency-safe reference adapter for tests and local process
// composition. Durable deployments use a SQL implementation with audit/outbox atomicity.
type MemoryRepository struct {
	mu              sync.RWMutex
	entitlements    map[string]Entitlement
	budgets         map[string]Budget
	reservations    map[string]Reservation
	byIdempotency   map[string]string
	maxReservations int
}

func NewMemoryRepository(maxReservations int) *MemoryRepository {
	if maxReservations <= 0 {
		maxReservations = 10_000
	}
	return &MemoryRepository{
		entitlements:    make(map[string]Entitlement),
		budgets:         make(map[string]Budget),
		reservations:    make(map[string]Reservation),
		byIdempotency:   make(map[string]string),
		maxReservations: maxReservations,
	}
}

func policyKey(subject, workspace string) string { return canonicalJoin(subject, workspace) }

func idempotencyKey(reservation Reservation) string {
	return canonicalJoin(reservation.Idempotency.Scope.String(), reservation.Idempotency.Key.String())
}

func (repository *MemoryRepository) PutEntitlement(ctx context.Context, entitlement Entitlement) error {
	if ctx == nil {
		return invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := entitlement.Validate(); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := policyKey(entitlement.Subject, entitlement.Workspace)
	if current, exists := repository.entitlements[key]; exists && entitlement.Version.Generation() <= current.Version.Generation() {
		return conflict("entitlement_version_not_monotonic", "entitlement version must increase")
	}
	repository.entitlements[key] = entitlement.clone()
	return nil
}

func (repository *MemoryRepository) PutBudget(ctx context.Context, budget Budget) error {
	if ctx == nil {
		return invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := budget.Validate(); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if current, exists := repository.budgets[budget.Workspace]; exists && budget.Version.Generation() <= current.Version.Generation() {
		return conflict("budget_version_not_monotonic", "budget version must increase")
	}
	repository.budgets[budget.Workspace] = budget.clone()
	return nil
}

func (repository *MemoryRepository) Snapshot(ctx context.Context, subject, workspace string) (PolicySnapshot, error) {
	if ctx == nil {
		return PolicySnapshot{}, invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return PolicySnapshot{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	entitlement, entitled := repository.entitlements[policyKey(subject, workspace)]
	budget, budgeted := repository.budgets[workspace]
	if !entitled {
		return PolicySnapshot{}, notFound("entitlement_not_found", "entitlement was not found")
	}
	if !budgeted {
		return PolicySnapshot{}, notFound("budget_not_found", "budget was not found")
	}
	return PolicySnapshot{Entitlement: entitlement.clone(), Budget: budget.clone()}, nil
}

func (repository *MemoryRepository) Reserve(ctx context.Context, snapshot PolicySnapshot, candidate Reservation, now time.Time) (Reservation, bool, error) {
	if ctx == nil {
		return Reservation{}, false, invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return Reservation{}, false, err
	}
	if err := snapshot.Validate(); err != nil {
		return Reservation{}, false, err
	}
	if err := candidate.Validate(); err != nil {
		return Reservation{}, false, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()

	idempotency := idempotencyKey(candidate)
	if existingID, exists := repository.byIdempotency[idempotency]; exists {
		existing, found := repository.reservations[existingID]
		if !found {
			return Reservation{}, false, unavailable("idempotency_index_corrupt", "idempotency index references a missing reservation", nil)
		}
		if !existing.SameAdmissionRequest(candidate) {
			return Reservation{}, false, conflict("idempotency_payload_mismatch", "idempotency key was reused for a different request")
		}
		if existing.State == ReservationReserved && !now.Before(existing.ExpiresAt) {
			updated, err := existing.Expire(now)
			if err != nil {
				return Reservation{}, false, err
			}
			repository.reservations[existingID] = updated
			existing = updated
		}
		if existing.State == ReservationExpired {
			return Reservation{}, false, failedPrecondition("reservation_expired", "idempotent reservation has expired")
		}
		return existing.clone(), true, nil
	}
	if len(repository.reservations) >= repository.maxReservations {
		return Reservation{}, false, exhausted("reservation_store_bound", "reservation store bound was reached")
	}
	currentEntitlement, entitled := repository.entitlements[policyKey(candidate.Subject, candidate.Workspace)]
	currentBudget, budgeted := repository.budgets[candidate.Workspace]
	if !entitled || !budgeted {
		return Reservation{}, false, conflict("policy_snapshot_stale", "policy snapshot is stale")
	}
	if currentEntitlement.ID.String() != snapshot.Entitlement.ID.String() ||
		currentEntitlement.Version.String() != snapshot.Entitlement.Version.String() ||
		currentBudget.ID.String() != snapshot.Budget.ID.String() ||
		currentBudget.Version.String() != snapshot.Budget.Version.String() {
		return Reservation{}, false, conflict("policy_snapshot_stale", "policy snapshot is stale")
	}
	currentSnapshot := PolicySnapshot{Entitlement: currentEntitlement, Budget: currentBudget}
	if err := candidate.ValidateInitial(currentSnapshot, now); err != nil {
		return Reservation{}, false, err
	}
	if !currentEntitlement.ActiveAt(now) || !currentBudget.ActiveAt(now) {
		return Reservation{}, false, failedPrecondition("policy_window_inactive", "policy window is inactive")
	}
	if currentEntitlement.PolicyEpoch != candidate.PolicyEpoch || !currentEntitlement.Allows(candidate.Route) ||
		!candidate.Reserved.Fits(currentEntitlement.MaximumRequest) {
		return Reservation{}, false, denied("entitlement_changed", "entitlement no longer allows the request")
	}
	if _, exists := repository.reservations[candidate.ID.String()]; exists {
		return Reservation{}, false, conflict("reservation_id_conflict", "reservation ID already exists")
	}

	used := make(Quota)
	for id, reservation := range repository.reservations {
		if reservation.BudgetID.String() != currentBudget.ID.String() {
			continue
		}
		if reservation.State == ReservationReserved && !now.Before(reservation.ExpiresAt) {
			updated, err := reservation.Expire(now)
			if err != nil {
				return Reservation{}, false, err
			}
			repository.reservations[id] = updated
			reservation = updated
		}
		amount := Quota(nil)
		switch reservation.State {
		case ReservationReserved:
			amount = reservation.Reserved
		case ReservationCommitted:
			amount = reservation.Actual
		}
		var err error
		used, err = used.add(amount)
		if err != nil {
			return Reservation{}, false, unavailable("budget_ledger_corrupt", "budget ledger is invalid", err)
		}
	}
	projected, err := used.add(candidate.Reserved)
	if err != nil || !projected.Fits(currentBudget.Limit) {
		return Reservation{}, false, exhausted("budget_exhausted", "budget cannot satisfy the reservation")
	}
	repository.reservations[candidate.ID.String()] = candidate.clone()
	repository.byIdempotency[idempotency] = candidate.ID.String()
	return candidate.clone(), false, nil
}

func (repository *MemoryRepository) Commit(ctx context.Context, id identifiers.ID, expected resourceversion.Version, requestDigest identifiers.Digest, subject string, actual Quota, now time.Time) (Reservation, bool, error) {
	return repository.finalize(ctx, id, expected, requestDigest, subject, actual, ReservationCommitted, now)
}

func (repository *MemoryRepository) Release(ctx context.Context, id identifiers.ID, expected resourceversion.Version, requestDigest identifiers.Digest, subject string, now time.Time) (Reservation, bool, error) {
	return repository.finalize(ctx, id, expected, requestDigest, subject, nil, ReservationReleased, now)
}

func (repository *MemoryRepository) finalize(ctx context.Context, id identifiers.ID, expected resourceversion.Version, requestDigest identifiers.Digest, subject string, actual Quota, target ReservationState, now time.Time) (Reservation, bool, error) {
	if ctx == nil {
		return Reservation{}, false, invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return Reservation{}, false, err
	}
	if err := validateID(id, "reservation", "reservation_id"); err != nil {
		return Reservation{}, false, err
	}
	if err := expected.Validate(); err != nil || !requestDigest.Valid() {
		return Reservation{}, false, invalid("finalization_identity_invalid", "finalization identity is invalid", err)
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	current, exists := repository.reservations[id.String()]
	if !exists {
		return Reservation{}, false, notFound("reservation_not_found", "reservation was not found")
	}
	if !current.RequestDigest.Equal(requestDigest) {
		return Reservation{}, false, denied("request_digest_mismatch", "request digest does not own reservation")
	}
	if current.Subject != subject {
		return Reservation{}, false, denied("reservation_subject_mismatch", "subject does not own reservation")
	}
	if current.State == ReservationExpired {
		return Reservation{}, false, failedPrecondition("reservation_expired", "reservation has expired")
	}
	if current.State == target && (target != ReservationCommitted || current.Actual.Equal(actual)) {
		return current.clone(), true, nil
	}
	if current.State != ReservationReserved {
		return Reservation{}, false, conflict("reservation_terminal", "reservation is already terminal")
	}
	if current.Version.String() != expected.String() {
		return Reservation{}, false, conflict("reservation_version_stale", "reservation version is stale")
	}
	if !now.Before(current.ExpiresAt) {
		updated, err := current.Expire(now)
		if err != nil {
			return Reservation{}, false, err
		}
		repository.reservations[id.String()] = updated
		return Reservation{}, false, failedPrecondition("reservation_expired", "reservation has expired")
	}
	var err error
	if target == ReservationCommitted {
		current, err = current.Commit(actual, now)
	} else {
		current, err = current.Release(now)
	}
	if err != nil {
		return Reservation{}, false, err
	}
	repository.reservations[id.String()] = current
	return current.clone(), false, nil
}

func (repository *MemoryRepository) Get(ctx context.Context, id identifiers.ID) (Reservation, error) {
	if ctx == nil {
		return Reservation{}, invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return Reservation{}, err
	}
	if err := validateID(id, "reservation", "reservation_id"); err != nil {
		return Reservation{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	reservation, exists := repository.reservations[id.String()]
	if !exists {
		return Reservation{}, notFound("reservation_not_found", "reservation was not found")
	}
	return reservation.clone(), nil
}
