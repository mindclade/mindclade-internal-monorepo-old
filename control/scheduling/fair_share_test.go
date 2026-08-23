// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import (
	"reflect"
	"testing"
)

func testShare(t *testing.T, claims ...ShareClaim) FairShare {
	t.Helper()
	share := FairShare{
		Domain:   mustDomain(t, WorkloadClassBatchCPU),
		Capacity: cpuDemand(12_000, 12*gibibyte, 12*gibibyte, 12),
		Claims:   claims,
	}
	if err := share.Validate(); err != nil {
		t.Fatal(err)
	}
	return share
}

func TestOnlyTwoPriorityClassesExist(t *testing.T) {
	classes := PriorityClasses()
	if len(classes) != 2 {
		t.Fatalf("expected two priority classes, got %d", len(classes))
	}
	if PriorityPlatformCritical.Value() != 1_000_000 || PriorityBatch.Value() != 10_000 {
		t.Fatal("priority class values do not match infra/kubernetes/base/priority-classes.yaml")
	}
	if PriorityClass("mindclade-urgent").Valid() {
		t.Fatal("an undeclared priority class must not validate")
	}
}

// mindclade-batch declares preemptionPolicy Never. Go-side policy must mirror
// it, or this package plans evictions the cluster will not honour.
func TestBatchNeverPreempts(t *testing.T) {
	if PriorityBatch.MayPreempt() {
		t.Fatal("batch declares preemptionPolicy Never and must never preempt")
	}
	if PriorityBatch.Preempts(PriorityBatch) || PriorityBatch.Preempts(PriorityPlatformCritical) {
		t.Fatal("batch must not outrank anything")
	}
	if !PriorityPlatformCritical.Preempts(PriorityBatch) {
		t.Fatal("platform-critical declares PreemptLowerPriority and must outrank batch")
	}
	// Kubernetes preempts strictly lower priority only.
	if PriorityPlatformCritical.Preempts(PriorityPlatformCritical) {
		t.Fatal("equal priority must not preempt")
	}
}

// The fair-share penalty is bounded well inside the gap between the two class
// values, so no amount of over-use can push a batch item above platform work.
func TestFairSharePenaltyNeverInvertsPriorityClasses(t *testing.T) {
	worstPlatform, err := QueuePriority(PriorityPlatformCritical, ShareScale)
	if err != nil {
		t.Fatal(err)
	}
	bestBatch, err := QueuePriority(PriorityBatch, 0)
	if err != nil {
		t.Fatal(err)
	}
	if bestBatch >= worstPlatform {
		t.Fatalf("an unpenalised batch item (%d) outranked a fully penalised platform item (%d)", bestBatch, worstPlatform)
	}
	if worstPlatform != PriorityPlatformCritical.Value()-PriorityShareBand {
		t.Fatalf("unexpected penalty at full share: %d", worstPlatform)
	}
	if bestBatch != PriorityBatch.Value() {
		t.Fatalf("a tenant with no usage must not be penalised, got %d", bestBatch)
	}
	if _, err := QueuePriority(PriorityBatch, ShareScale+1); err == nil {
		t.Fatal("a share position outside bounds must be rejected")
	}
	if _, err := QueuePriority(PriorityClass("mindclade-urgent"), 0); err == nil {
		t.Fatal("an undeclared priority class must be rejected")
	}
}

func TestFairShareOrdersLeastServedFirst(t *testing.T) {
	share := testShare(t,
		ShareClaim{Tenant: "heavy", Weight: 1, Used: cpuDemand(9_000, gibibyte, gibibyte, 1)},
		ShareClaim{Tenant: "light", Weight: 1, Used: cpuDemand(1_000, gibibyte, gibibyte, 1)},
		ShareClaim{Tenant: "idle", Weight: 1, Used: cpuDemand(0, 0, 0, 0)},
	)
	order, err := share.Order()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"idle", "light", "heavy"}) {
		t.Fatalf("expected least-served first, got %v", order)
	}
}

// A heavier weight buys proportionally more usage before the tenant is
// considered equally served.
func TestFairShareOrderIsWeighted(t *testing.T) {
	share := testShare(t,
		ShareClaim{Tenant: "big", Weight: 4, Used: cpuDemand(4_000, gibibyte, gibibyte, 1)},
		ShareClaim{Tenant: "small", Weight: 1, Used: cpuDemand(2_000, gibibyte, gibibyte, 1)},
	)
	order, err := share.Order()
	if err != nil {
		t.Fatal(err)
	}
	// big holds twice as much but has four times the weight, so it is the
	// less-served of the two.
	if !reflect.DeepEqual(order, []string{"big", "small"}) {
		t.Fatalf("expected the weighted ordering, got %v", order)
	}
}

// Two replicas evaluating the same snapshot must pick the same tenant, or a
// leadership handover reorders the queue every time it moves.
func TestFairShareOrderBreaksTiesDeterministically(t *testing.T) {
	claims := []ShareClaim{
		{Tenant: "zulu", Weight: 1, Used: cpuDemand(1_000, gibibyte, gibibyte, 1)},
		{Tenant: "alpha", Weight: 1, Used: cpuDemand(1_000, gibibyte, gibibyte, 1)},
		{Tenant: "mike", Weight: 1, Used: cpuDemand(1_000, gibibyte, gibibyte, 1)},
	}
	share := testShare(t, claims...)
	expected := []string{"alpha", "mike", "zulu"}
	for attempt := 0; attempt < 8; attempt++ {
		order, err := share.Order()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(order, expected) {
			t.Fatalf("attempt %d produced %v, expected %v", attempt, order, expected)
		}
	}
}

func TestFairShareEntitlementFloorsWithinCapacity(t *testing.T) {
	share := testShare(t,
		ShareClaim{Tenant: "a", Weight: 1},
		ShareClaim{Tenant: "b", Weight: 2},
	)
	first, err := share.Entitlement("a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := share.Entitlement("b")
	if err != nil {
		t.Fatal(err)
	}
	if first[ResourceCPU] != 4_000 || second[ResourceCPU] != 8_000 {
		t.Fatalf("expected a 1:2 split of twelve cores, got %d and %d", first[ResourceCPU], second[ResourceCPU])
	}
	total, err := first.add(second)
	if err != nil {
		t.Fatal(err)
	}
	// Flooring must never over-promise: the summed entitlements have to fit.
	if !total.Fits(share.Capacity) {
		t.Fatalf("summed entitlements %v exceed capacity %v", total, share.Capacity)
	}
	unknown, err := share.Entitlement("nobody")
	if err != nil {
		t.Fatal(err)
	}
	if !unknown.IsZero() {
		t.Fatalf("an unweighted tenant is owed nothing, got %v", unknown)
	}
}

func TestFairSharePositionSaturatesAndIsWeighted(t *testing.T) {
	share := testShare(t,
		ShareClaim{Tenant: "over", Weight: 1, Used: cpuDemand(12_000, gibibyte, gibibyte, 1)},
		ShareClaim{Tenant: "under", Weight: 1, Used: cpuDemand(3_000, gibibyte, gibibyte, 1)},
	)
	saturated, err := share.Position("over")
	if err != nil {
		t.Fatal(err)
	}
	if saturated != ShareScale {
		t.Fatalf("a tenant at capacity must saturate, got %d", saturated)
	}
	partial, err := share.Position("under")
	if err != nil {
		t.Fatal(err)
	}
	if partial == 0 || partial >= ShareScale {
		t.Fatalf("a partially served tenant must fall strictly between the bounds, got %d", partial)
	}
	absent, err := share.Position("nobody")
	if err != nil {
		t.Fatal(err)
	}
	if absent != 0 {
		t.Fatalf("a tenant with no claim is entirely unserved, got %d", absent)
	}
}

func TestFairShareAdmitsWithinEntitlementAndRefusesBeyondItWhenContended(t *testing.T) {
	share := testShare(t,
		ShareClaim{Tenant: "a", Weight: 1, Used: cpuDemand(0, 0, 0, 0)},
		ShareClaim{Tenant: "b", Weight: 1, Used: cpuDemand(0, 0, 0, 0)},
	)
	within, err := share.Admits("a", cpuDemand(6_000, 6*gibibyte, 6*gibibyte, 6))
	if err != nil {
		t.Fatal(err)
	}
	if !within {
		t.Fatal("a request inside the entitlement must be admitted")
	}
	beyond, err := share.Admits("a", cpuDemand(7_000, gibibyte, gibibyte, 1))
	if err != nil {
		t.Fatal(err)
	}
	if beyond {
		t.Fatal("a request past the entitlement must not be admitted by fair share")
	}
	contended, err := share.Contended("a")
	if err != nil {
		t.Fatal(err)
	}
	if !contended {
		t.Fatal("an idle peer below its share is contention")
	}
}

func TestFairShareIsUncontendedWhenEveryPeerIsAtItsShare(t *testing.T) {
	share := testShare(t,
		ShareClaim{Tenant: "a", Weight: 1, Used: cpuDemand(0, 0, 0, 0)},
		ShareClaim{Tenant: "b", Weight: 1, Used: cpuDemand(6_000, 6*gibibyte, 6*gibibyte, 6)},
	)
	contended, err := share.Contended("a")
	if err != nil {
		t.Fatal(err)
	}
	if contended {
		t.Fatal("a peer holding its full entitlement is not starved")
	}
}

func TestFairShareWithoutWeightsConstrainsNothing(t *testing.T) {
	share := testShare(t)
	admits, err := share.Admits("a", cpuDemand(12_000, 12*gibibyte, 12*gibibyte, 12))
	if err != nil {
		t.Fatal(err)
	}
	if !admits {
		t.Fatal("with no weighted claims fairness has nothing to divide")
	}
	if _, err := share.Entitlement("a"); err == nil {
		t.Fatal("an unweighted share has no entitlement to compute")
	}
}

func TestFairShareValidationRejectsMalformedClaims(t *testing.T) {
	domain := mustDomain(t, WorkloadClassBatchCPU)
	mutators := map[string]func(*FairShare){
		"tenant_invalid":            func(s *FairShare) { s.Claims[0].Tenant = "Research Team" },
		"share_weight_out_of_range": func(s *FairShare) { s.Claims[0].Weight = 0 },
		"fair_share_duplicate_tenant": func(s *FairShare) {
			s.Claims = append(s.Claims, ShareClaim{Tenant: s.Claims[0].Tenant, Weight: 1})
		},
		"demand_resource_uncovered": func(s *FairShare) { s.Claims[0].Used = Demand{ResourceGPU: 1, ResourcePods: 1} },
	}
	for reason, mutate := range mutators {
		t.Run(reason, func(t *testing.T) {
			share := FairShare{
				Domain:   domain,
				Capacity: cpuDemand(12_000, 12*gibibyte, 12*gibibyte, 12),
				Claims:   []ShareClaim{{Tenant: "research", Weight: 1, Used: cpuDemand(1_000, gibibyte, gibibyte, 1)}},
			}
			mutate(&share)
			expectReason(t, share.Validate(), reason)
		})
	}
}

func TestFairShareCloneDoesNotAliasItsSource(t *testing.T) {
	share := testShare(t, ShareClaim{Tenant: "research", Weight: 1, Used: cpuDemand(1_000, gibibyte, gibibyte, 1)})
	clone := share.Clone()
	clone.Claims[0].Used[ResourceCPU] = 9_999
	clone.Capacity[ResourceCPU] = 1
	if share.Claims[0].Used[ResourceCPU] != 1_000 || share.Capacity[ResourceCPU] != 12_000 {
		t.Fatal("mutating a clone reached back into its source")
	}
}
