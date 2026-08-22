// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admission

import (
	"context"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

func newPolicyService(t *testing.T) (PolicyService, *MemoryRepository, *clock.FakeClock) {
	t.Helper()
	now := clock.NewFake(testStart)
	ids, err := identifiers.NewGenerator(identifiers.WithTimeSource(now.Now))
	if err != nil {
		t.Fatal(err)
	}
	repository := NewMemoryRepository(100)
	return PolicyService{Repository: repository, IDs: ids, Clock: now}, repository, now
}

func entitlementSpec(epoch uint64) EntitlementSpec {
	return EntitlementSpec{
		Subject: "service-account", Workspace: "research-team", PolicyEpoch: epoch,
		Routes:         []GatewayRoute{{Endpoint: "chat-primary", Provider: "vertex", Model: "gemini-pro"}},
		MaximumRequest: Quota{UnitRequests: 1, UnitInputTokens: 2_000, UnitOutputTokens: 1_000, UnitCostMicros: 5_000},
		NotBefore:      testStart.Add(-time.Minute), ExpiresAt: testStart.Add(time.Hour),
	}
}

func budgetSpec(startsAt, expiresAt time.Time, requests uint64) BudgetSpec {
	return BudgetSpec{
		Workspace: "research-team",
		Limit:     Quota{UnitRequests: requests, UnitInputTokens: 20_000, UnitOutputTokens: 10_000, UnitCostMicros: 50_000},
		StartsAt:  startsAt, ExpiresAt: expiresAt,
	}
}

func TestPolicyServiceCreatesAndUpdatesSealedEntitlement(t *testing.T) {
	service, repository, _ := newPolicyService(t)
	ctx := context.Background()
	created, wasCreated, err := service.ApplyEntitlement(ctx, entitlementSpec(7), resourceversion.RequireAbsence())
	if err != nil {
		t.Fatal(err)
	}
	if !wasCreated || created.ID.Kind().String() != "entitlement" || created.Version.Generation() != 1 {
		t.Fatalf("unexpected created entitlement: %+v", created)
	}
	updatedSpec := entitlementSpec(8)
	updatedSpec.MaximumRequest[UnitInputTokens] = 3_000
	updated, wasCreated, err := service.ApplyEntitlement(ctx, updatedSpec, resourceversion.MatchVersion(created.Version))
	if err != nil {
		t.Fatal(err)
	}
	if wasCreated || updated.ID != created.ID || updated.Version.Generation() != 2 {
		t.Fatalf("unexpected updated entitlement: %+v", updated)
	}
	stored, err := repository.GetEntitlement(ctx, updated.Subject, updated.Workspace)
	if err != nil || stored.Version != updated.Version {
		t.Fatalf("stored entitlement=%+v error=%v", stored, err)
	}
	if _, _, err := service.ApplyEntitlement(ctx, entitlementSpec(9), resourceversion.MatchVersion(created.Version)); !faults.IsReason(err, "resource_precondition_failed") {
		t.Fatalf("expected stale precondition failure, got %v", err)
	}
	if _, _, err := service.ApplyEntitlement(ctx, entitlementSpec(10), resourceversion.MatchVersion(updated.Version)); !faults.IsReason(err, "policy_epoch_not_monotonic") {
		t.Fatalf("expected exact epoch increment failure, got %v", err)
	}
}

func TestPolicyServiceBudgetWindowLifecycle(t *testing.T) {
	service, _, now := newPolicyService(t)
	ctx := context.Background()
	startsAt := testStart.Add(-time.Minute)
	expiresAt := testStart.Add(time.Hour)
	created, wasCreated, err := service.ApplyBudget(ctx, budgetSpec(startsAt, expiresAt, 10), resourceversion.RequireAbsence())
	if err != nil {
		t.Fatal(err)
	}
	if !wasCreated || created.ID.Kind().String() != "budget" || created.Version.Generation() != 1 {
		t.Fatalf("unexpected created budget: %+v", created)
	}
	adjusted, wasCreated, err := service.ApplyBudget(ctx, budgetSpec(startsAt, expiresAt, 20), resourceversion.MatchVersion(created.Version))
	if err != nil {
		t.Fatal(err)
	}
	if wasCreated || adjusted.ID != created.ID || adjusted.Version.Generation() != 2 {
		t.Fatalf("unexpected adjusted budget: %+v", adjusted)
	}
	nextStart := expiresAt
	nextExpiry := nextStart.Add(time.Hour)
	if _, _, err := service.ApplyBudget(ctx, budgetSpec(nextStart, nextExpiry, 30), resourceversion.MatchVersion(adjusted.Version)); !faults.IsReason(err, "budget_window_active") {
		t.Fatalf("expected active-window replacement failure, got %v", err)
	}
	if err := now.Set(nextStart); err != nil {
		t.Fatal(err)
	}
	replacement, wasCreated, err := service.ApplyBudget(ctx, budgetSpec(nextStart, nextExpiry, 30), resourceversion.MatchVersion(adjusted.Version))
	if err != nil {
		t.Fatal(err)
	}
	if wasCreated || replacement.ID == adjusted.ID || replacement.Version.Generation() != 3 || !replacement.ActiveAt(now.Now()) {
		t.Fatalf("unexpected replacement budget: %+v", replacement)
	}
}

func TestPolicyServiceRequiresExactPrecondition(t *testing.T) {
	service, _, _ := newPolicyService(t)
	for name, precondition := range map[string]resourceversion.Precondition{"missing": {}, "wildcard match": resourceversion.RequireExistence()} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := service.ApplyEntitlement(context.Background(), entitlementSpec(1), precondition); !faults.IsReason(err, "policy_precondition_not_exact") {
				t.Fatalf("expected exact precondition failure, got %v", err)
			}
		})
	}
}

func TestPolicyServiceCannotReduceBudgetBelowDurableUsage(t *testing.T) {
	policies, repository, value := newPolicyService(t)
	ctx := context.Background()
	if _, _, err := policies.ApplyEntitlement(ctx, entitlementSpec(7), resourceversion.RequireAbsence()); err != nil {
		t.Fatal(err)
	}
	budget, _, err := policies.ApplyBudget(ctx, budgetSpec(testStart.Add(-time.Minute), testStart.Add(time.Hour), 2), resourceversion.RequireAbsence())
	if err != nil {
		t.Fatal(err)
	}
	service := Service{Repository: repository, Clock: value, MaximumTTL: time.Minute}
	if _, err := service.Admit(ctx, request(91)); err != nil {
		t.Fatal(err)
	}
	reduced := budgetSpec(budget.StartsAt, budget.ExpiresAt, 0)
	if _, _, err := policies.ApplyBudget(ctx, reduced, resourceversion.MatchVersion(budget.Version)); !faults.IsReason(err, "budget_limit_below_usage") {
		t.Fatalf("expected durable usage floor, got %v", err)
	}
	stored, err := policies.Budget(ctx, budget.Workspace)
	if err != nil || stored.Version != budget.Version || stored.Limit[UnitRequests] != 2 {
		t.Fatalf("failed reduction changed durable budget: %+v error=%v", stored, err)
	}
}
