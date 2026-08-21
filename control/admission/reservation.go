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
	ReservationReserved  ReservationState = "reserved"
	ReservationCommitted ReservationState = "committed"
	ReservationReleased  ReservationState = "released"
	ReservationExpired   ReservationState = "expired"
)

func (state ReservationState) Valid() bool {
	switch state {
	case ReservationReserved, ReservationCommitted, ReservationReleased, ReservationExpired:
		return true
	default:
		return false
	}
}

// Reservation is the immutable-versioned authorization consumed by a Gateway caller. It does
// not contain a provider credential or authorize a Mindclade model release/deployment.
type Reservation struct {
	ID                 identifiers.ID
	Idempotency        idempotency.Identity
	RequestDigest      identifiers.Digest
	Subject            string
	Workspace          string
	Route              GatewayRoute
	PolicyEpoch        uint64
	EntitlementID      identifiers.ID
	EntitlementVersion resourceversion.Version
	BudgetID           identifiers.ID
	BudgetVersion      resourceversion.Version
	Reserved           Quota
	Actual             Quota
	State              ReservationState
	CreatedAt          time.Time
	ExpiresAt          time.Time
	FinalizedAt        time.Time
	Version            resourceversion.Version
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
	if !r.State.Valid() {
		return invalid("reservation_state_invalid", "reservation state is invalid", nil)
	}
	if r.CreatedAt.IsZero() || !r.ExpiresAt.After(r.CreatedAt) {
		return invalid("reservation_window_invalid", "reservation window is invalid", nil)
	}
	if r.State == ReservationCommitted {
		if err := r.Actual.Validate(false); err != nil {
			return err
		}
		if !r.Actual.Fits(r.Reserved) || r.FinalizedAt.IsZero() {
			return invalid("reservation_actual_invalid", "committed amount is invalid", nil)
		}
	} else if len(r.Actual) != 0 {
		return invalid("reservation_actual_unexpected", "non-committed reservation has actual usage", nil)
	}
	if r.State != ReservationReserved && r.FinalizedAt.IsZero() {
		return invalid("reservation_finalized_at_missing", "terminal reservation lacks finalization time", nil)
	}
	if err := r.Version.Validate(); err != nil {
		return invalid("reservation_version_invalid", "reservation version is invalid", err)
	}
	return nil
}

func (r Reservation) clone() Reservation {
	r.Reserved = r.Reserved.Clone()
	r.Actual = r.Actual.Clone()
	return r
}

// Commit returns the next immutable reservation version with actual usage
// recorded. The actual amount may be smaller than the reservation but can
// never exceed it in any dimension.
func (r Reservation) Commit(actual Quota, finalizedAt time.Time) (Reservation, error) {
	if err := actual.Validate(false); err != nil {
		return Reservation{}, err
	}
	if !actual.Fits(r.Reserved) {
		return Reservation{}, invalid("actual_exceeds_reservation", "actual usage exceeds reservation", nil)
	}
	return r.transition(ReservationCommitted, actual, finalizedAt)
}

// Release returns the next immutable reservation version without charging
// usage. Only a currently reserved authorization can be released.
func (r Reservation) Release(finalizedAt time.Time) (Reservation, error) {
	return r.transition(ReservationReleased, nil, finalizedAt)
}

// Expire returns the next immutable reservation version after its deadline.
// Calling it before ExpiresAt is a failed precondition.
func (r Reservation) Expire(finalizedAt time.Time) (Reservation, error) {
	if finalizedAt.Before(r.ExpiresAt) {
		return Reservation{}, failedPrecondition("reservation_not_expired", "reservation has not expired")
	}
	return r.transition(ReservationExpired, nil, finalizedAt)
}

func (r Reservation) transition(state ReservationState, actual Quota, finalizedAt time.Time) (Reservation, error) {
	if err := r.Validate(); err != nil {
		return Reservation{}, err
	}
	if r.State != ReservationReserved {
		return Reservation{}, conflict("reservation_terminal", "reservation is already terminal")
	}
	if finalizedAt.IsZero() || finalizedAt.Before(r.CreatedAt) {
		return Reservation{}, invalid("reservation_finalized_at_invalid", "reservation finalization time is invalid", nil)
	}
	r.State = state
	r.Actual = actual.Clone()
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
		r.ID.String(), r.Idempotency.Scope.String(), r.Idempotency.Key.String(),
		r.RequestDigest.String(), r.Subject, r.Workspace, r.Route.Endpoint, r.Route.Provider,
		r.Route.Model, strconv.FormatUint(r.PolicyEpoch, 10), r.EntitlementID.String(),
		r.EntitlementVersion.String(), r.BudgetID.String(), r.BudgetVersion.String(),
		r.Reserved.canonical(), r.Actual.canonical(), string(r.State),
		r.CreatedAt.UTC().Format(time.RFC3339Nano), r.ExpiresAt.UTC().Format(time.RFC3339Nano),
		r.FinalizedAt.UTC().Format(time.RFC3339Nano),
	))
}

func versionReservation(r Reservation, generation uint64) (Reservation, error) {
	version, err := resourceversion.New(generation, reservationDigest(r))
	if err != nil {
		return Reservation{}, err
	}
	r.Version = version
	return r, nil
}
