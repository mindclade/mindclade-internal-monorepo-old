// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import (
	"testing"

	"go.mindclade.dev/control/orchestration"
)

// allStageKinds is the closed set orchestration declares. It is written out
// rather than derived so that adding a twelfth stage kind upstream fails this
// test instead of silently leaving the new kind unmapped.
var allStageKinds = []orchestration.StageKind{
	orchestration.StageIngestion,
	orchestration.StageCurate,
	orchestration.StagePreprocess,
	orchestration.StageReferenceBuild,
	orchestration.StageBatchInference,
	orchestration.StageEvaluation,
	orchestration.StageTraining,
	orchestration.StageCheckpointTransfer,
	orchestration.StageArtifactTransfer,
	orchestration.StageRollout,
	orchestration.StageSimulation,
}

func TestPoolForStageIsTotalOverEveryStageKind(t *testing.T) {
	if len(allStageKinds) != 11 {
		t.Fatalf("expected eleven stage kinds, got %d", len(allStageKinds))
	}
	for _, kind := range allStageKinds {
		if !kind.Valid() {
			t.Fatalf("stage kind %q is not valid upstream", kind)
		}
		pool, err := PoolForStage(kind)
		if err != nil {
			t.Fatalf("stage kind %q has no resource class: %v", kind, err)
		}
		if !pool.Valid() {
			t.Fatalf("stage kind %q mapped to an unknown pool %q", kind, pool)
		}
		if pool.ExpenseRank() < 0 {
			t.Fatalf("pool %q has no expense rank", pool)
		}
	}
	if _, err := PoolForStage(orchestration.StageKind("teleport")); err == nil {
		t.Fatal("an unknown stage kind must not resolve to a resource class")
	}
}

func TestSevenPoolsAreDeclared(t *testing.T) {
	pools := Pools()
	if len(pools) != 7 {
		t.Fatalf("expected seven resource pools, got %d", len(pools))
	}
	seen := make(map[PoolKind]struct{}, len(pools))
	for _, pool := range pools {
		if !pool.Valid() {
			t.Fatalf("pool %q is not valid", pool)
		}
		if _, exists := seen[pool]; exists {
			t.Fatalf("pool %q is declared twice", pool)
		}
		seen[pool] = struct{}{}
	}
}

// The reference search pool must rank below every accelerated pool, or the
// generic upstream invariant stops covering the GPU reservation rule.
func TestAcceleratedPoolsOutrankReferenceSearch(t *testing.T) {
	search := PoolSearchCPU.ExpenseRank()
	for _, pool := range Pools() {
		if !pool.Accelerated() {
			continue
		}
		if pool.ExpenseRank() <= search {
			t.Fatalf("accelerated pool %q ranks at or below reference search", pool)
		}
	}
	if PoolArtifactDataPlane.ExpenseRank() >= search {
		t.Fatal("the artifact/data plane must be cheaper to hold than reference search")
	}
}

func TestExactlyThreeCapacityTriplesExist(t *testing.T) {
	domains := Domains()
	if len(domains) != 3 {
		t.Fatalf("expected three capacity domains, got %d", len(domains))
	}
	expected := map[WorkloadClass][3]string{
		WorkloadClassBatchCPU:     {"mindclade-batch-cpu", "mindclade-batch-cpu", "batch-cpu"},
		WorkloadClassTrainingH100: {"mindclade-training-h100", "mindclade-training-h100", "training-h100"},
		WorkloadClassTrainingB200: {"mindclade-training-b200", "mindclade-training-b200", "training-b200"},
	}
	for _, domain := range domains {
		triple, known := expected[domain.WorkloadClass()]
		if !known {
			t.Fatalf("domain %s is not one of the three admissible triples", domain)
		}
		if domain.Namespace() != triple[0] || domain.QueueName() != triple[1] || string(domain.WorkloadClass()) != triple[2] {
			t.Fatalf("domain %s does not match the cluster triple %v", domain, triple)
		}
		if domain.ClusterQueue() != triple[1] {
			t.Fatalf("domain %s points at a different cluster queue", domain)
		}
	}
}

// A free-form queue name is denied at the API server, so it must be
// unrepresentable here. The namespace spelling is the most likely mistake and
// is rejected explicitly.
func TestCapacityDomainRejectsAnythingButTheThreeClasses(t *testing.T) {
	for _, value := range []string{"", "batch", "mindclade-batch-cpu", "training-h200", "BATCH-CPU"} {
		if _, err := DomainFor(WorkloadClass(value)); err == nil {
			t.Fatalf("workload class %q must not resolve to a capacity domain", value)
		}
	}
	if (CapacityDomain{}).Validate() == nil {
		t.Fatal("the zero capacity domain must not validate")
	}
	if !(CapacityDomain{}).IsZero() {
		t.Fatal("the zero capacity domain must report itself as zero")
	}
}

func TestCapacityDomainTextRoundTrip(t *testing.T) {
	for _, domain := range Domains() {
		encoded, err := domain.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var decoded CapacityDomain
		if err := decoded.UnmarshalText(encoded); err != nil {
			t.Fatal(err)
		}
		if decoded != domain {
			t.Fatalf("round trip changed %s into %s", domain, decoded)
		}
	}
	var decoded CapacityDomain
	if err := decoded.UnmarshalText([]byte("mindclade-batch-cpu")); err == nil {
		t.Fatal("a namespace is not a workload class and must not decode")
	}
	if _, err := (CapacityDomain{}).MarshalText(); err == nil {
		t.Fatal("the zero capacity domain must not encode")
	}
}

func TestDomainFlavorAndAcceleratorAgree(t *testing.T) {
	cases := map[Accelerator]ResourceFlavor{
		AcceleratorNone: FlavorCPUGeneralOnDemand,
		AcceleratorH100: FlavorH100,
		AcceleratorB200: FlavorB200,
	}
	for accelerator, flavor := range cases {
		domain, err := DomainForAccelerator(accelerator)
		if err != nil {
			t.Fatal(err)
		}
		if domain.Flavor() != flavor {
			t.Fatalf("accelerator %q resolved to flavor %q, expected %q", accelerator, domain.Flavor(), flavor)
		}
		if domain.Accelerator() != accelerator {
			t.Fatalf("domain %s does not round-trip its accelerator", domain)
		}
		if flavor.TopologyAware() != (accelerator != AcceleratorNone) {
			t.Fatalf("flavor %q topology awareness does not match its accelerator", flavor)
		}
	}
	if _, err := DomainForAccelerator(Accelerator("tpu")); err == nil {
		t.Fatal("an unknown accelerator must not resolve to a capacity domain")
	}
}

func TestResolvePoolRefusesAcceleratorsForNonAcceleratedPools(t *testing.T) {
	for _, pool := range Pools() {
		if pool.Accelerated() {
			continue
		}
		if _, err := ResolvePool(pool, AcceleratorH100); err == nil {
			t.Fatalf("pool %q must not be able to hold accelerator capacity", pool)
		}
		resolved, err := ResolvePool(pool, AcceleratorNone)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.Domain.WorkloadClass() != WorkloadClassBatchCPU {
			t.Fatalf("pool %q resolved outside the batch-cpu domain", pool)
		}
	}
	accelerated, err := ResolvePool(PoolGPUTraining, AcceleratorB200)
	if err != nil {
		t.Fatal(err)
	}
	if accelerated.Flavor != FlavorB200 {
		t.Fatalf("expected the B200 flavor, got %q", accelerated.Flavor)
	}
	if err := accelerated.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPoolRejectsAnInconsistentBinding(t *testing.T) {
	pool, err := ResolvePool(PoolGPUTraining, AcceleratorH100)
	if err != nil {
		t.Fatal(err)
	}
	tampered := pool
	tampered.Flavor = FlavorB200
	if err := tampered.Validate(); err == nil {
		t.Fatal("a pool whose flavor contradicts its domain must not validate")
	}
	if _, err := ResolvePool(PoolKind("mystery"), AcceleratorNone); err == nil {
		t.Fatal("an unknown pool must not resolve")
	}
}
