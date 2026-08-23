// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import "testing"

func TestOneResourceGroupCoversFiveResources(t *testing.T) {
	if len(coveredResources) != 5 {
		t.Fatalf("expected five covered resources, got %d", len(coveredResources))
	}
	for _, name := range coveredResources {
		if !name.Valid() {
			t.Fatalf("covered resource %q is not valid", name)
		}
	}
	if ResourceName("nvidia.com/mig").Valid() {
		t.Fatal("a resource outside the group must not validate")
	}
}

// The batch-cpu ClusterQueue omits nvidia.com/gpu from its coveredResources, so
// GPU is not a dimension that domain can charge at all.
func TestBatchDomainDoesNotCoverAccelerators(t *testing.T) {
	batch := mustDomain(t, WorkloadClassBatchCPU)
	if batch.Covers(ResourceGPU) {
		t.Fatal("the batch-cpu domain must not cover nvidia.com/gpu")
	}
	if len(batch.CoveredResources()) != 4 {
		t.Fatalf("expected four covered resources in the batch domain, got %d", len(batch.CoveredResources()))
	}
	for _, class := range []WorkloadClass{WorkloadClassTrainingH100, WorkloadClassTrainingB200} {
		domain := mustDomain(t, class)
		if !domain.Covers(ResourceGPU) {
			t.Fatalf("the %s domain must cover nvidia.com/gpu", class)
		}
		if len(domain.CoveredResources()) != 5 {
			t.Fatalf("expected five covered resources in the %s domain", class)
		}
	}
	if err := gpuDemand(1_000, gibibyte, gibibyte, 1, 1).ValidateForDomain(batch, true); err == nil {
		t.Fatal("a GPU demand must be denied in the batch-cpu domain")
	}
}

func TestDemandValidationRejectsMalformedVectors(t *testing.T) {
	mutators := map[string]func(Demand) Demand{
		"demand_resource_invalid": func(demand Demand) Demand {
			demand[ResourceName("nvidia.com/mig")] = 1
			return demand
		},
		"demand_amount_out_of_range": func(demand Demand) Demand {
			demand[ResourceCPU] = MaximumResourceAmount + 1
			return demand
		},
		"demand_empty": func(Demand) Demand { return Demand{} },
	}
	for reason, mutate := range mutators {
		t.Run(reason, func(t *testing.T) {
			expectReason(t, mutate(cpuDemand(1_000, gibibyte, gibibyte, 1)).Validate(true), reason)
		})
	}
}

func TestDemandArithmeticIsOverflowChecked(t *testing.T) {
	near := Demand{ResourceCPU: MaximumResourceAmount}
	if _, err := near.add(Demand{ResourceCPU: 1}); err == nil {
		t.Fatal("a sum past the bound must not be produced")
	}
	if _, err := near.scale(2); err == nil {
		t.Fatal("a scaled amount past the bound must not be produced")
	}
	if _, err := (Demand{ResourceCPU: 1}).Scale(0); err == nil {
		t.Fatal("a zero scale factor must be rejected")
	}
	sum, err := cpuDemand(1_000, gibibyte, gibibyte, 1).add(cpuDemand(2_000, gibibyte, gibibyte, 1))
	if err != nil {
		t.Fatal(err)
	}
	if sum[ResourceCPU] != 3_000 || sum[ResourcePods] != 2 {
		t.Fatalf("unexpected sum %v", sum)
	}
}

// Releasing more than was reserved means the ledger and the reservation
// disagree. Clamping at zero would hide the divergence and leak the difference.
func TestDemandSubtractionRefusesToGoNegative(t *testing.T) {
	_, err := cpuDemand(1_000, gibibyte, gibibyte, 1).sub(cpuDemand(2_000, gibibyte, gibibyte, 1))
	expectReason(t, err, "demand_underflow")
}

func TestDemandFitsIsWholeVector(t *testing.T) {
	limit := cpuDemand(4_000, 4*gibibyte, 4*gibibyte, 4)
	if !cpuDemand(4_000, 4*gibibyte, 4*gibibyte, 4).Fits(limit) {
		t.Fatal("an exactly equal demand must fit")
	}
	// One dimension over is the whole vector over: the ResourceGroup is
	// admitted or refused as a unit.
	if cpuDemand(4_000, 4*gibibyte, 4*gibibyte, 5).Fits(limit) {
		t.Fatal("a demand exceeding one dimension must not fit")
	}
	if !(Demand{}).IsZero() || cpuDemand(1, 1, 1, 1).IsZero() {
		t.Fatal("IsZero does not agree with the vector contents")
	}
}

func TestAllocatableReserveAndFreeRoundTrip(t *testing.T) {
	batch := mustDomain(t, WorkloadClassBatchCPU)
	allocatable := Allocatable{Domain: batch, Nominal: cpuDemand(8_000, 8*gibibyte, 8*gibibyte, 8)}
	demand := cpuDemand(2_000, 2*gibibyte, 2*gibibyte, 2)
	reserved, err := allocatable.Reserve(demand)
	if err != nil {
		t.Fatal(err)
	}
	available, err := reserved.Available()
	if err != nil {
		t.Fatal(err)
	}
	if available[ResourceCPU] != 6_000 {
		t.Fatalf("expected six cores available, got %v", available)
	}
	freed, err := reserved.Free(demand)
	if err != nil {
		t.Fatal(err)
	}
	if !freed.Reserved.IsZero() {
		t.Fatalf("freeing the whole reservation must empty the ledger, got %v", freed.Reserved)
	}
	if allocatable.Reserved != nil {
		t.Fatal("Reserve mutated its receiver")
	}
}

func TestAllocatableRefusesToOverCommit(t *testing.T) {
	batch := mustDomain(t, WorkloadClassBatchCPU)
	allocatable := Allocatable{Domain: batch, Nominal: cpuDemand(4_000, 4*gibibyte, 4*gibibyte, 4)}
	_, err := allocatable.Reserve(cpuDemand(5_000, gibibyte, gibibyte, 1))
	expectReason(t, err, "capacity_exhausted")

	over := Allocatable{
		Domain:   batch,
		Nominal:  cpuDemand(4_000, 4*gibibyte, 4*gibibyte, 4),
		Reserved: cpuDemand(5_000, gibibyte, gibibyte, 1),
	}
	expectReason(t, over.Validate(), "allocatable_over_reserved")
}

func TestDominantShareUsesTheScarcestDimension(t *testing.T) {
	capacity := cpuDemand(10_000, 10*gibibyte, 10*gibibyte, 10)
	// Half the CPU, a tenth of everything else: the dominant share is the CPU.
	share := cpuDemand(5_000, gibibyte, gibibyte, 1).DominantShare(capacity)
	if share != ShareScale/2 {
		t.Fatalf("expected a dominant share of %d, got %d", ShareScale/2, share)
	}
	if got := cpuDemand(20_000, gibibyte, gibibyte, 1).DominantShare(capacity); got != ShareScale {
		t.Fatalf("an over-capacity demand must saturate, got %d", got)
	}
	// A dimension with no capacity is not a dimension this domain competes on.
	if got := (Demand{ResourceGPU: 4}).DominantShare(capacity); got != 0 {
		t.Fatalf("an uncovered dimension must not contribute, got %d", got)
	}
	if got := (Demand{}).DominantShare(capacity); got != 0 {
		t.Fatalf("an empty demand occupies nothing, got %d", got)
	}
}

func TestAllocatableUtilizationTracksReservations(t *testing.T) {
	batch := mustDomain(t, WorkloadClassBatchCPU)
	allocatable := Allocatable{
		Domain:   batch,
		Nominal:  cpuDemand(8_000, 8*gibibyte, 8*gibibyte, 8),
		Reserved: cpuDemand(2_000, gibibyte, gibibyte, 1),
	}
	if got := allocatable.Utilization(); got != ShareScale/4 {
		t.Fatalf("expected a quarter utilization, got %d", got)
	}
}
