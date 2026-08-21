// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admission

import (
	"context"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

const (
	MinimumReservationTTL = time.Second
	DefaultMaximumTTL     = 15 * time.Minute
)

// AdmitRequest is trusted only after the service resolves server-owned entitlement and budget
// state. RequestDigest must bind the provider request without exposing its prompt or payload.
type AdmitRequest struct {
	Idempotency   idempotency.Identity
	RequestDigest identifiers.Digest
	Subject       string
	Workspace     string
	Route         GatewayRoute
	PolicyEpoch   uint64
	Requested     Quota
	TTL           time.Duration
}

func (request AdmitRequest) Validate(maximumTTL time.Duration) error {
	if err := request.Idempotency.Validate(); err != nil {
		return invalid("idempotency_invalid", "idempotency identity is invalid", err)
	}
	if !request.RequestDigest.Valid() {
		return invalid("request_digest_invalid", "request digest is required", nil)
	}
	if err := validateName(request.Subject, "subject"); err != nil {
		return err
	}
	if err := validateName(request.Workspace, "workspace"); err != nil {
		return err
	}
	expectedScope := request.Workspace + "/mlflow-gateway/" + request.Subject
	if request.Idempotency.Scope.String() != expectedScope {
		return invalid("idempotency_scope_mismatch", "idempotency scope does not match subject", nil)
	}
	if err := request.Route.Validate(); err != nil {
		return err
	}
	if request.PolicyEpoch == 0 {
		return invalid("policy_epoch_invalid", "policy epoch is required", nil)
	}
	if err := request.Requested.Validate(true); err != nil {
		return err
	}
	if request.TTL < MinimumReservationTTL || request.TTL > maximumTTL {
		return invalid("reservation_ttl_invalid", "reservation TTL is outside bounds", nil)
	}
	return nil
}

type Decision struct {
	Reservation Reservation
	Replayed    bool
}

type Service struct {
	Repository Repository
	Clock      clock.Clock
	MaximumTTL time.Duration
}

func (service Service) Admit(ctx context.Context, request AdmitRequest) (Decision, error) {
	if ctx == nil {
		return Decision{}, invalid("context_nil", "context is required", nil)
	}
	if service.Repository == nil {
		return Decision{}, unavailable("repository_unavailable", "admission repository is unavailable", nil)
	}
	maximumTTL := service.MaximumTTL
	if maximumTTL == 0 {
		maximumTTL = DefaultMaximumTTL
	}
	if maximumTTL < MinimumReservationTTL || maximumTTL > 24*time.Hour {
		return Decision{}, invalid("maximum_ttl_invalid", "service maximum TTL is outside bounds", nil)
	}
	if err := request.Validate(maximumTTL); err != nil {
		return Decision{}, err
	}
	now := service.now()
	snapshot, err := service.Repository.Snapshot(ctx, request.Subject, request.Workspace)
	if err != nil {
		return Decision{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return Decision{}, err
	}
	if !snapshot.Entitlement.ActiveAt(now) {
		return Decision{}, denied("entitlement_inactive", "entitlement is not active")
	}
	if snapshot.Entitlement.PolicyEpoch != request.PolicyEpoch {
		return Decision{}, denied("policy_epoch_stale", "request policy epoch is stale")
	}
	if !snapshot.Entitlement.Allows(request.Route) {
		return Decision{}, denied("route_not_entitled", "route is not entitled")
	}
	if !request.Requested.Fits(snapshot.Entitlement.MaximumRequest) {
		return Decision{}, denied("request_exceeds_entitlement", "request exceeds entitlement")
	}
	if !snapshot.Budget.ActiveAt(now) {
		return Decision{}, exhausted("budget_inactive", "budget window is not active")
	}

	expiresAt := now.Add(request.TTL)
	if snapshot.Entitlement.ExpiresAt.Before(expiresAt) {
		expiresAt = snapshot.Entitlement.ExpiresAt
	}
	if snapshot.Budget.ExpiresAt.Before(expiresAt) {
		expiresAt = snapshot.Budget.ExpiresAt
	}
	if !expiresAt.After(now) {
		return Decision{}, failedPrecondition("reservation_window_empty", "reservation window is empty")
	}
	reservationID, err := identifiers.NewIDAt(identifiers.MustParseKind("reservation"), now)
	if err != nil {
		return Decision{}, unavailable("reservation_id_unavailable", "reservation ID is unavailable", err)
	}
	candidate := Reservation{
		ID:                 reservationID,
		Idempotency:        request.Idempotency,
		RequestDigest:      request.RequestDigest,
		Subject:            request.Subject,
		Workspace:          request.Workspace,
		Route:              request.Route,
		PolicyEpoch:        request.PolicyEpoch,
		EntitlementID:      snapshot.Entitlement.ID,
		EntitlementVersion: snapshot.Entitlement.Version,
		BudgetID:           snapshot.Budget.ID,
		BudgetVersion:      snapshot.Budget.Version,
		Reserved:           request.Requested.Clone(),
		State:              ReservationReserved,
		CreatedAt:          now,
		ExpiresAt:          expiresAt,
	}
	candidate, err = versionReservation(candidate, 1)
	if err != nil {
		return Decision{}, unavailable("reservation_version_unavailable", "reservation version is unavailable", err)
	}
	reservation, replayed, err := service.Repository.Reserve(ctx, snapshot, candidate, now)
	if err != nil {
		return Decision{}, err
	}
	return Decision{Reservation: reservation, Replayed: replayed}, nil
}

func (service Service) Commit(ctx context.Context, id identifiers.ID, expected resourceversion.Version, requestDigest identifiers.Digest, actual Quota) (Decision, error) {
	if ctx == nil {
		return Decision{}, invalid("context_nil", "context is required", nil)
	}
	if service.Repository == nil {
		return Decision{}, unavailable("repository_unavailable", "admission repository is unavailable", nil)
	}
	if err := actual.Validate(false); err != nil {
		return Decision{}, err
	}
	reservation, replayed, err := service.Repository.Commit(ctx, id, expected, requestDigest, actual, service.now())
	return Decision{Reservation: reservation, Replayed: replayed}, err
}

func (service Service) Release(ctx context.Context, id identifiers.ID, expected resourceversion.Version, requestDigest identifiers.Digest) (Decision, error) {
	if ctx == nil {
		return Decision{}, invalid("context_nil", "context is required", nil)
	}
	if service.Repository == nil {
		return Decision{}, unavailable("repository_unavailable", "admission repository is unavailable", nil)
	}
	reservation, replayed, err := service.Repository.Release(ctx, id, expected, requestDigest, service.now())
	return Decision{Reservation: reservation, Replayed: replayed}, err
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return clock.RealClock{}.Now().UTC()
	}
	return service.Clock.Now().UTC()
}
