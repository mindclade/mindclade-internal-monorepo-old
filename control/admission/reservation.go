// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admission

import (
	"strconv"
	"time"

	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

type ReservationState string

const (
	ReservationReserved              ReservationState = "reserved"
	ReservationDispatched            ReservationState = "dispatched"
	ReservationReconciliationPending ReservationState = "reconciliation_pending"
	ReservationCommitted             ReservationState = "committed"
	ReservationReleased              ReservationState = "released"
	ReservationExpired               ReservationState = "expired"
)

func (state ReservationState) Valid() bool {
	switch state {
	case ReservationReserved, ReservationDispatched, ReservationReconciliationPending,
		ReservationCommitted, ReservationReleased, ReservationExpired:
		return true
	default:
		return false
	}
}

// Reservation is the immutable-versioned authorization consumed by a Gateway caller. It does
// not contain a provider credential or authorize a Mindclade model release/deployment.
type Reservation struct {
	ID                 identifiers.ID          `json:"id"`
	Idempotency        idempotency.Identity    `json:"idempotency"`
	RequestDigest      identifiers.Digest      `json:"request_digest"`
	Subject            string                  `json:"subject"`
	Workspace          string                  `json:"workspace"`
	Route              GatewayRoute            `json:"route"`
	PolicyEpoch        uint64                  `json:"policy_epoch"`
	EntitlementID      identifiers.ID          `json:"entitlement_id"`
	EntitlementVersion resourceversion.Version `json:"entitlement_version"`
	BudgetID           identifiers.ID          `json:"budget_id"`
	BudgetVersion      resourceversion.Version `json:"budget_version"`
	Reserved           Quota                   `json:"reserved"`
	RequestedTTL       time.Duration           `json:"requested_ttl_nanoseconds"`
	Actual             Quota                   `json:"actual,omitempty"`
	State              ReservationState        `json:"state"`
	CreatedAt          time.Time               `json:"created_at"`
	ExpiresAt          time.Time               `json:"expires_at"`
	DispatchedAt       time.Time               `json:"dispatched_at,omitempty"`
	ReconciliationAt   time.Time               `json:"reconciliation_pending_at,omitempty"`
	FinalizedAt        time.Time               `json:"finalized_at,omitempty"`
	Version            resourceversion.Version `json:"resource_version"`
}

func (r Reservation) Validate() error {
	if err := validateID(r.ID, "reservation", "reservation_id"); err != nil {
		return err
	}
	if err := r.Idempotency.Validate(); err != nil {
		return invalid("reservation_idempotency_invalid", "reservation idempotency is invalid", err)
	}
	if !r.RequestDigest.Valid() {
		return invalid("request_digest_invalid", "request digest is required", nil)
	}
	if err := validateName(r.Subject, "subject"); err != nil {
		return err
	}
	if err := validateName(r.Workspace, "workspace"); err != nil {
		return err
	}
	if err := r.Route.Validate(); err != nil {
		return err
	}
	if r.PolicyEpoch == 0 {
		return invalid("policy_epoch_invalid", "policy epoch is required", nil)
	}
	if err := validateID(r.EntitlementID, "entitlement", "entitlement_id"); err != nil {
		return err
	}
	if err := r.EntitlementVersion.Validate(); err != nil {
		return invalid("entitlement_version_invalid", "entitlement version is invalid", err)
	}
	if err := validateID(r.BudgetID, "budget", "budget_id"); err != nil {
		return err
	}
	if err := r.BudgetVersion.Validate(); err != nil {
		return invalid("budget_version_invalid", "budget version is invalid", err)
	}
	if err := r.Reserved.Validate(true); err != nil {
		return err
	}
	if r.Reserved[UnitRequests] != 1 {
		return invalid("reservation_request_count_invalid", "a gateway reservation must authorize exactly one request", nil)
	}
	if r.RequestedTTL < MinimumReservationTTL || r.RequestedTTL > 24*time.Hour {
		return invalid("reservation_ttl_invalid", "reservation TTL is outside bounds", nil)
	}
	if !r.State.Valid() {
		return invalid("reservation_state_invalid", "reservation state is invalid", nil)
	}
	if r.CreatedAt.IsZero() || !r.ExpiresAt.After(r.CreatedAt) {
		return invalid("reservation_window_invalid", "reservation window is invalid", nil)
	}
	if !r.DispatchedAt.IsZero() && (r.DispatchedAt.Before(r.CreatedAt) || !r.DispatchedAt.Before(r.ExpiresAt)) {
		return invalid("reservation_dispatch_time_invalid", "reservation dispatch time is invalid", nil)
	}
	if !r.ReconciliationAt.IsZero() && (r.DispatchedAt.IsZero() || r.ReconciliationAt.Before(r.DispatchedAt)) {
		return invalid("reservation_reconciliation_time_invalid", "reservation reconciliation time is invalid", nil)
	}
	switch r.State {
	case ReservationReserved:
		if len(r.Actual) != 0 || !r.DispatchedAt.IsZero() || !r.ReconciliationAt.IsZero() || !r.FinalizedAt.IsZero() {
			return invalid("reservation_reserved_lifecycle_invalid", "reserved reservation has unexpected lifecycle state", nil)
		}
	case ReservationDispatched:
		if len(r.Actual) != 0 || r.DispatchedAt.IsZero() || !r.ReconciliationAt.IsZero() || !r.FinalizedAt.IsZero() {
			return invalid("reservation_dispatched_lifecycle_invalid", "dispatched reservation has invalid lifecycle state", nil)
		}
	case ReservationReconciliationPending:
		if len(r.Actual) != 0 || r.DispatchedAt.IsZero() || r.ReconciliationAt.IsZero() || !r.FinalizedAt.IsZero() {
			return invalid("reservation_reconciliation_lifecycle_invalid", "reconciliation-pending reservation has invalid lifecycle state", nil)
		}
	case ReservationCommitted:
		if err := r.Actual.Validate(false); err != nil {
			return err
		}
		if r.Actual[UnitRequests] != 1 || !r.Actual.Fits(r.Reserved) || r.DispatchedAt.IsZero() || r.FinalizedAt.IsZero() ||
			r.FinalizedAt.Before(r.DispatchedAt) || !r.ReconciliationAt.IsZero() && r.FinalizedAt.Before(r.ReconciliationAt) {
			return invalid("reservation_actual_invalid", "committed amount is invalid", nil)
		}
	case ReservationReleased:
		if len(r.Actual) != 0 || !r.DispatchedAt.IsZero() || !r.ReconciliationAt.IsZero() || r.FinalizedAt.IsZero() ||
			r.FinalizedAt.Before(r.CreatedAt) || !r.FinalizedAt.Before(r.ExpiresAt) {
			return invalid("reservation_release_lifecycle_invalid", "released reservation has invalid lifecycle state", nil)
		}
	case ReservationExpired:
		if len(r.Actual) != 0 || !r.DispatchedAt.IsZero() || !r.ReconciliationAt.IsZero() || r.FinalizedAt.IsZero() {
			return invalid("reservation_expiration_lifecycle_invalid", "expired reservation has invalid lifecycle state", nil)
		}
		if r.FinalizedAt.Before(r.ExpiresAt) {
			return invalid("reservation_expiration_time_invalid", "expired reservation was finalized before its deadline", nil)
		}
	}
	if err := r.Version.Validate(); err != nil {
		return invalid("reservation_version_invalid", "reservation version is invalid", err)
	}
	expectedGeneration := uint64(1)
	switch r.State {
	case ReservationDispatched, ReservationReleased, ReservationExpired:
		expectedGeneration = 2
	case ReservationReconciliationPending:
		expectedGeneration = 3
	case ReservationCommitted:
		expectedGeneration = 3
		if !r.ReconciliationAt.IsZero() {
			expectedGeneration = 4
		}
	}
	if r.Version.Generation() != expectedGeneration {
		return invalid("reservation_version_generation_invalid", "reservation generation does not match its state", nil)
	}
	if !r.Version.Digest().Equal(reservationDigest(r)) {
		return invalid("reservation_version_digest_mismatch", "reservation version does not seal its content", nil)
	}
	return nil
}

func (r Reservation) clone() Reservation {
	r.Reserved = r.Reserved.Clone()
	r.Actual = r.Actual.Clone()
	return r
}

// ValidateInitial verifies that a newly created reservation is an exact,
// generation-one projection of the policy snapshot at the transaction time.
// Repository implementations call this after locking the current policy.
func (r Reservation) ValidateInitial(snapshot PolicySnapshot, now time.Time) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	if err := r.Validate(); err != nil {
		return err
	}
	if r.EntitlementID.String() != snapshot.Entitlement.ID.String() ||
		r.EntitlementVersion.String() != snapshot.Entitlement.Version.String() ||
		r.BudgetID.String() != snapshot.Budget.ID.String() ||
		r.BudgetVersion.String() != snapshot.Budget.Version.String() {
		return conflict("reservation_policy_mismatch", "reservation is not bound to the current policy")
	}
	if r.State != ReservationReserved || len(r.Actual) != 0 || !r.FinalizedAt.IsZero() {
		return invalid("reservation_initial_state_invalid", "reservation is not in its initial state", nil)
	}
	if !r.CreatedAt.Equal(now) {
		return invalid("reservation_created_at_invalid", "reservation creation time does not match the transaction time", nil)
	}
	expectedExpiry := now.Add(r.RequestedTTL)
	if snapshot.Entitlement.ExpiresAt.Before(expectedExpiry) {
		expectedExpiry = snapshot.Entitlement.ExpiresAt
	}
	if snapshot.Budget.ExpiresAt.Before(expectedExpiry) {
		expectedExpiry = snapshot.Budget.ExpiresAt
	}
	if !r.ExpiresAt.Equal(expectedExpiry) {
		return invalid("reservation_expiry_invalid", "reservation expiry does not match the authorized window", nil)
	}
	expected, err := versionReservation(r, 1)
	if err != nil {
		return unavailable("reservation_version_unavailable", "reservation version is unavailable", err)
	}
	if r.Version.String() != expected.Version.String() {
		return invalid("reservation_version_invalid", "reservation initial version does not bind its content", nil)
	}
	return nil
}

// Dispatch durably crosses the no-charge boundary immediately before provider
// I/O. Once dispatched, a reservation can never be released or expire without
// charge.
func (r Reservation) Dispatch(at time.Time) (Reservation, error) {
	if err := r.Validate(); err != nil {
		return Reservation{}, err
	}
	if r.State != ReservationReserved {
		return Reservation{}, conflict("reservation_dispatch_invalid", "only a reserved authorization can be dispatched")
	}
	at = at.Round(0).UTC()
	if at.Before(r.CreatedAt) || !at.Before(r.ExpiresAt) {
		return Reservation{}, failedPrecondition("reservation_expired", "reservation has expired")
	}
	r = r.clone()
	r.State = ReservationDispatched
	r.DispatchedAt = at
	return sealReservationTransition(r)
}

// MarkReconciliationPending records that a dispatched provider outcome is
// ambiguous. A reconciler must terminally max-charge it.
func (r Reservation) MarkReconciliationPending(at time.Time) (Reservation, error) {
	if err := r.Validate(); err != nil {
		return Reservation{}, err
	}
	if r.State != ReservationDispatched {
		return Reservation{}, conflict("reservation_reconciliation_invalid", "only a dispatched authorization can await reconciliation")
	}
	at = at.Round(0).UTC()
	if at.Before(r.DispatchedAt) {
		return Reservation{}, invalid("reservation_reconciliation_time_invalid", "reservation reconciliation time is invalid", nil)
	}
	r = r.clone()
	r.State = ReservationReconciliationPending
	r.ReconciliationAt = at
	return sealReservationTransition(r)
}

// Commit returns the next immutable reservation version with actual usage
// recorded. Only work that crossed the durable dispatch boundary can commit.
func (r Reservation) Commit(actual Quota, finalizedAt time.Time) (Reservation, error) {
	if err := r.Validate(); err != nil {
		return Reservation{}, err
	}
	if err := actual.Validate(false); err != nil {
		return Reservation{}, err
	}
	if !actual.Fits(r.Reserved) {
		return Reservation{}, invalid("actual_exceeds_reservation", "actual usage exceeds reservation", nil)
	}
	if actual[UnitRequests] != 1 {
		return Reservation{}, invalid("actual_request_count_invalid", "committed gateway usage must contain exactly one request", nil)
	}
	if r.State != ReservationDispatched && r.State != ReservationReconciliationPending {
		return Reservation{}, conflict("reservation_commit_without_dispatch", "only dispatched work can be committed")
	}
	finalizedAt = finalizedAt.Round(0).UTC()
	minimum := r.DispatchedAt
	if !r.ReconciliationAt.IsZero() {
		minimum = r.ReconciliationAt
	}
	if finalizedAt.Before(minimum) {
		return Reservation{}, invalid("reservation_finalized_at_invalid", "reservation finalization time is invalid", nil)
	}
	r = r.clone()
	r.State = ReservationCommitted
	r.Actual = actual.Clone()
	r.FinalizedAt = finalizedAt
	return sealReservationTransition(r)
}

func sealReservationTransition(reservation Reservation) (Reservation, error) {
	updated, err := versionReservation(reservation, reservation.Version.Generation()+1)
	if err != nil {
		return Reservation{}, unavailable("reservation_version_unavailable", "reservation version is unavailable", err)
	}
	if err := updated.Validate(); err != nil {
		return Reservation{}, err
	}
	return updated, nil
}

// Reconcile terminally charges the full reservation when provider execution
// may have occurred but exact usage is unknowable.
func (r Reservation) Reconcile(finalizedAt time.Time) (Reservation, error) {
	if r.State != ReservationReconciliationPending {
		return Reservation{}, conflict("reservation_reconciliation_invalid", "only reconciliation-pending work can be max-charged")
	}
	return r.Commit(r.Reserved, finalizedAt)
}

// Release returns the next immutable reservation version without charging
// usage. Only a currently reserved authorization can be released.
func (r Reservation) Release(finalizedAt time.Time) (Reservation, error) {
	return r.transitionUndispatched(ReservationReleased, finalizedAt)
}

// Expire returns the next immutable reservation version after its deadline.
// Calling it before ExpiresAt is a failed precondition.
func (r Reservation) Expire(finalizedAt time.Time) (Reservation, error) {
	if finalizedAt.Before(r.ExpiresAt) {
		return Reservation{}, failedPrecondition("reservation_not_expired", "reservation has not expired")
	}
	return r.transitionUndispatched(ReservationExpired, finalizedAt)
}

func (r Reservation) transitionUndispatched(state ReservationState, finalizedAt time.Time) (Reservation, error) {
	if err := r.Validate(); err != nil {
		return Reservation{}, err
	}
	if r.State != ReservationReserved {
		return Reservation{}, conflict("reservation_already_dispatched", "only an undispatched reservation can be released or expired")
	}
	if finalizedAt.IsZero() || finalizedAt.Before(r.CreatedAt) {
		return Reservation{}, invalid("reservation_finalized_at_invalid", "reservation finalization time is invalid", nil)
	}
	if state == ReservationExpired {
		if finalizedAt.Before(r.ExpiresAt) {
			return Reservation{}, failedPrecondition("reservation_not_expired", "reservation has not expired")
		}
	} else if !finalizedAt.Before(r.ExpiresAt) {
		return Reservation{}, failedPrecondition("reservation_expired", "reservation has expired")
	}
	r = r.clone()
	r.State = state
	r.FinalizedAt = finalizedAt.Round(0).UTC()
	updated, err := versionReservation(r, r.Version.Generation()+1)
	if err != nil {
		return Reservation{}, unavailable("reservation_version_unavailable", "reservation version is unavailable", err)
	}
	if err := updated.Validate(); err != nil {
		return Reservation{}, err
	}
	return updated, nil
}

func reservationDigest(r Reservation) identifiers.Digest {
	return identifiers.SHA256String(canonicalJoin(
		"gateway-reservation-v1", r.ID.String(), r.Idempotency.Scope.String(), r.Idempotency.Key.String(),
		r.RequestDigest.String(), r.Subject, r.Workspace, r.Route.Endpoint, r.Route.Provider,
		r.Route.Model, strconv.FormatUint(r.PolicyEpoch, 10), r.EntitlementID.String(),
		r.EntitlementVersion.String(), r.BudgetID.String(), r.BudgetVersion.String(),
		r.Reserved.canonical(), strconv.FormatInt(int64(r.RequestedTTL), 10),
		r.Actual.canonical(), string(r.State),
		r.CreatedAt.UTC().Format(time.RFC3339Nano), r.ExpiresAt.UTC().Format(time.RFC3339Nano),
		r.DispatchedAt.UTC().Format(time.RFC3339Nano), r.ReconciliationAt.UTC().Format(time.RFC3339Nano),
		r.FinalizedAt.UTC().Format(time.RFC3339Nano),
	))
}

// SameAdmissionRequest compares only caller-owned fields bound to an idempotency key. Generated
// identity, transaction timestamps, and server-resolved policy versions are deliberately excluded
// so a retry after policy rotation returns the original decision.
func (r Reservation) SameAdmissionRequest(other Reservation) bool {
	return r.RequestDigest.Equal(other.RequestDigest) && r.Subject == other.Subject &&
		r.Workspace == other.Workspace && r.Route == other.Route &&
		r.PolicyEpoch == other.PolicyEpoch && r.Reserved.Equal(other.Reserved) &&
		r.RequestedTTL == other.RequestedTTL
}

func versionReservation(r Reservation, generation uint64) (Reservation, error) {
	version, err := resourceversion.New(generation, reservationDigest(r))
	if err != nil {
		return Reservation{}, err
	}
	r.Version = version
	return r, nil
}
