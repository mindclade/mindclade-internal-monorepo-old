// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admission

import (
	"context"
	"math"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

// PolicyRepository is the narrow administrative storage contract. Reservation
// callers never receive it; only the separately deployed admin composition can
// publish server-sealed policy.
type PolicyRepository interface {
	GetEntitlement(context.Context, string, string) (Entitlement, error)
	PutEntitlement(context.Context, Entitlement) error
	GetBudget(context.Context, string) (Budget, error)
	PutBudget(context.Context, Budget, ...time.Time) error
}

// EntitlementSpec is the caller-owned portion of an entitlement. Identity,
// generation, and the resource-version seal are always server-owned.
type EntitlementSpec struct {
	Subject        string         `json:"subject"`
	Workspace      string         `json:"workspace"`
	PolicyEpoch    uint64         `json:"policy_epoch"`
	Routes         []GatewayRoute `json:"routes"`
	MaximumRequest Quota          `json:"maximum_request"`
	NotBefore      time.Time      `json:"not_before"`
	ExpiresAt      time.Time      `json:"expires_at"`
}

// BudgetSpec is the caller-owned portion of a workspace budget. Budget IDs
// change only at a non-overlapping window rollover.
type BudgetSpec struct {
	Workspace string    `json:"workspace"`
	Limit     Quota     `json:"limit"`
	StartsAt  time.Time `json:"starts_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// PolicyService owns policy identity, generation, and seal creation. Callers
// provide an unsealed spec plus either RequireAbsence or an exact MatchVersion;
// wildcard overwrite is deliberately unsupported.
type PolicyService struct {
	Repository PolicyRepository
	IDs        *identifiers.Generator
	Clock      clock.Clock
}

func (service PolicyService) Entitlement(ctx context.Context, subject, workspace string) (Entitlement, error) {
	const operation = "admission.PolicyService.Entitlement"
	if err := service.validate(ctx, operation); err != nil {
		return Entitlement{}, err
	}
	if err := validateName(subject, "subject"); err != nil {
		return Entitlement{}, err
	}
	if err := validateName(workspace, "workspace"); err != nil {
		return Entitlement{}, err
	}
	return service.Repository.GetEntitlement(ctx, subject, workspace)
}

func (service PolicyService) Budget(ctx context.Context, workspace string) (Budget, error) {
	const operation = "admission.PolicyService.Budget"
	if err := service.validate(ctx, operation); err != nil {
		return Budget{}, err
	}
	if err := validateName(workspace, "workspace"); err != nil {
		return Budget{}, err
	}
	return service.Repository.GetBudget(ctx, workspace)
}

func (service PolicyService) ApplyEntitlement(ctx context.Context, spec EntitlementSpec, precondition resourceversion.Precondition) (Entitlement, bool, error) {
	const operation = "admission.PolicyService.ApplyEntitlement"
	if err := service.validate(ctx, operation); err != nil {
		return Entitlement{}, false, err
	}
	if err := exactPolicyPrecondition(precondition); err != nil {
		return Entitlement{}, false, err
	}
	if err := validateName(spec.Subject, "subject"); err != nil {
		return Entitlement{}, false, err
	}
	if err := validateName(spec.Workspace, "workspace"); err != nil {
		return Entitlement{}, false, err
	}
	current, err := service.Repository.GetEntitlement(ctx, spec.Subject, spec.Workspace)
	exists := err == nil
	if err != nil && !faults.IsCode(err, faults.CodeNotFound) {
		return Entitlement{}, false, err
	}
	currentVersion := resourceversion.Version{}
	if exists {
		currentVersion = current.Version
	}
	if err := precondition.Check(exists, currentVersion); err != nil {
		return Entitlement{}, false, err
	}

	generation := uint64(1)
	var id identifiers.ID
	if exists {
		if current.PolicyEpoch == math.MaxUint64 {
			return Entitlement{}, false, unavailable("policy_epoch_exhausted", "entitlement policy epoch is exhausted", nil)
		}
		if spec.PolicyEpoch != current.PolicyEpoch+1 {
			return Entitlement{}, false, conflict("policy_epoch_not_monotonic", "entitlement policy epoch must increase by one")
		}
		generation = current.Version.Generation() + 1
		if generation == 0 {
			return Entitlement{}, false, unavailable("entitlement_version_exhausted", "entitlement version is exhausted", nil)
		}
		id = current.ID
	} else {
		id, err = service.IDs.ID(identifiers.MustParseKind("entitlement"))
		if err != nil {
			return Entitlement{}, false, unavailable("entitlement_id_unavailable", "entitlement ID is unavailable", err)
		}
	}
	candidate, err := (Entitlement{
		ID: id, Subject: spec.Subject, Workspace: spec.Workspace, PolicyEpoch: spec.PolicyEpoch,
		Routes: spec.Routes, MaximumRequest: spec.MaximumRequest,
		NotBefore: spec.NotBefore.Round(0).UTC(), ExpiresAt: spec.ExpiresAt.Round(0).UTC(),
	}).Seal(generation)
	if err != nil {
		return Entitlement{}, false, err
	}
	if err := service.Repository.PutEntitlement(ctx, candidate); err != nil {
		return Entitlement{}, false, err
	}
	return candidate, !exists, nil
}

func (service PolicyService) ApplyBudget(ctx context.Context, spec BudgetSpec, precondition resourceversion.Precondition) (Budget, bool, error) {
	const operation = "admission.PolicyService.ApplyBudget"
	if err := service.validate(ctx, operation); err != nil {
		return Budget{}, false, err
	}
	if err := exactPolicyPrecondition(precondition); err != nil {
		return Budget{}, false, err
	}
	if err := validateName(spec.Workspace, "workspace"); err != nil {
		return Budget{}, false, err
	}
	spec.StartsAt = spec.StartsAt.Round(0).UTC()
	spec.ExpiresAt = spec.ExpiresAt.Round(0).UTC()
	current, err := service.Repository.GetBudget(ctx, spec.Workspace)
	exists := err == nil
	if err != nil && !faults.IsCode(err, faults.CodeNotFound) {
		return Budget{}, false, err
	}
	currentVersion := resourceversion.Version{}
	if exists {
		currentVersion = current.Version
	}
	if err := precondition.Check(exists, currentVersion); err != nil {
		return Budget{}, false, err
	}

	now := service.now()
	generation := uint64(1)
	var id identifiers.ID
	if exists {
		generation = current.Version.Generation() + 1
		if generation == 0 {
			return Budget{}, false, unavailable("budget_version_exhausted", "budget version is exhausted", nil)
		}
		if spec.StartsAt.Equal(current.StartsAt) && spec.ExpiresAt.Equal(current.ExpiresAt) {
			if !now.Before(current.ExpiresAt) {
				return Budget{}, false, failedPrecondition("budget_window_expired", "an expired budget window cannot be adjusted")
			}
			id = current.ID
		} else {
			if now.Before(current.ExpiresAt) {
				return Budget{}, false, failedPrecondition("budget_window_active", "an active budget window cannot be replaced")
			}
			if spec.StartsAt.Before(current.ExpiresAt) {
				return Budget{}, false, failedPrecondition("budget_window_overlap", "replacement budget window must not overlap the prior window")
			}
			if spec.StartsAt.After(now) || !spec.ExpiresAt.After(now) {
				return Budget{}, false, failedPrecondition("budget_rollover_window_invalid", "replacement budget must be active at publication time")
			}
			id, err = service.IDs.ID(identifiers.MustParseKind("budget"))
			if err != nil {
				return Budget{}, false, unavailable("budget_id_unavailable", "budget ID is unavailable", err)
			}
		}
	} else {
		if spec.StartsAt.After(now) || !spec.ExpiresAt.After(now) {
			return Budget{}, false, failedPrecondition("budget_initial_window_inactive", "initial budget must be active at publication time")
		}
		id, err = service.IDs.ID(identifiers.MustParseKind("budget"))
		if err != nil {
			return Budget{}, false, unavailable("budget_id_unavailable", "budget ID is unavailable", err)
		}
	}
	candidate, err := (Budget{
		ID: id, Workspace: spec.Workspace, Limit: spec.Limit,
		StartsAt: spec.StartsAt, ExpiresAt: spec.ExpiresAt,
	}).Seal(generation)
	if err != nil {
		return Budget{}, false, err
	}
	if err := service.Repository.PutBudget(ctx, candidate, now); err != nil {
		return Budget{}, false, err
	}
	return candidate, !exists, nil
}

func (service PolicyService) validate(ctx context.Context, operation string) error {
	if ctx == nil {
		return invalid("context_nil", "context is required", nil)
	}
	if nilInterface(service.Repository) || service.IDs == nil || nilInterface(service.Clock) {
		return unavailable("policy_service_unconfigured", "policy service is unavailable", nil)
	}
	if err := ctx.Err(); err != nil {
		return faults.Wrap(err, faults.CodeOf(err), faults.PublicMessageOf(err),
			faults.WithReason("policy_context_done"), faults.WithOperation(operation),
			faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}

func (service PolicyService) now() time.Time {
	return service.Clock.Now().Round(0).UTC()
}

func exactPolicyPrecondition(precondition resourceversion.Precondition) error {
	if err := precondition.Validate(); err != nil {
		return err
	}
	if precondition.MustExist || (precondition.Match.IsZero() && !precondition.MustNotExist) {
		return invalid("policy_precondition_not_exact", "policy mutation requires If-None-Match on create or exact If-Match on update", nil)
	}
	return nil
}
