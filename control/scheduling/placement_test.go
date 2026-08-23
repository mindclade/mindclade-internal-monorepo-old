// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import (
	"testing"

	"go.mindclade.dev/control/orchestration"
)

func upstream(t *testing.T, kind orchestration.StageKind, complete bool) UpstreamResourceStage {
	t.Helper()
	return UpstreamResourceStage{
		StageID:  mustID(t, "stage", testStart).String(),
		Kind:     kind,
		Complete: complete,
	}
}

func TestPlaceSelectsTheTripleFlavorAndTopology(t *testing.T) {
	f := newFixture(t)
	placement, err := f.snapshot(t).Place(placementFor(t, trainingRequest(t, "research")), testStart)
	if err != nil {
		t.Fatal(err)
	}
	if placement.Pool.Domain != f.training {
		t.Fatalf("expected the training-h100 domain, got %s", placement.Pool.Domain)
	}
	if placement.Pool.Flavor != FlavorH100 {
		t.Fatalf("expected the H100 flavor, got %s", placement.Pool.Flavor)
	}
	labels, err := QueueLabels(placement)
	if err != nil {
		t.Fatal(err)
	}
	if labels["kueue.x-k8s.io/queue-name"] != "mindclade-training-h100" ||
		labels["mindclade.dev/workload-class"] != "training-h100" {
		t.Fatalf("placement labels do not form the cluster triple: %v", labels)
	}
	if placement.Pool.Domain.Namespace() != "mindclade-training-h100" {
		t.Fatalf("placement namespace does not match its triple: %s", placement.Pool.Domain.Namespace())
	}
	if !placement.Digest.Equal(placementDigest(placement)) {
		t.Fatal("placement digest does not seal its content")
	}
}

// INVARIANT: "Scheduling must not hold an expensive downstream resource while
// an upstream resource-class stage is incomplete."
// docs/architecture/system-design-reference.md
func TestPlacementDoesNotHoldExpensiveDownstreamResourceWhileUpstreamStageIncomplete(t *testing.T) {
	f := newFixture(t)
	request := trainingRequest(t, "research")
	// Featurization is a different, cheaper resource class, and it has not
	// produced the training inputs yet.
	request.Upstream = []UpstreamResourceStage{upstream(t, orchestration.StagePreprocess, false)}
	_, err := f.snapshot(t).Place(placementFor(t, request), testStart)
	expectReason(t, err, "upstream_resource_class_incomplete")

	// The direct policy check answers the same way outside of admission.
	expectReason(t,
		EnforceUpstreamResourceClass(PoolGPUTraining, request.Upstream),
		"upstream_resource_class_incomplete")

	// Completing it releases the placement, and nothing else about the request
	// changed.
	request.Upstream = []UpstreamResourceStage{upstream(t, orchestration.StagePreprocess, true)}
	if _, err := f.snapshot(t).Place(placementFor(t, request), testStart); err != nil {
		t.Fatalf("a completed upstream stage must release the placement: %v", err)
	}
}

// INVARIANT: "No inference GPU or model slot is reserved while MSA/template/
// reference search is pending." docs/architecture/system-design-reference.md
// section 19.1, ADR-0013.
func TestNoInferenceGPUReservedWhileReferenceSearchPending(t *testing.T) {
	f := newFixture(t)
	pending := []UpstreamResourceStage{upstream(t, orchestration.StageReferenceBuild, false)}

	inference := AdmissionRequest{
		WorkloadID:  mustID(t, "workload", testStart),
		Tenant:      "research",
		Workspace:   "research-team",
		StageKind:   orchestration.StageBatchInference,
		Pool:        PoolGPUInference,
		Accelerator: AcceleratorH100,
		Priority:    PriorityPlatformCritical,
		Demand:      gpuDemand(4_000, 32*gibibyte, 64*gibibyte, 1, 1),
		Replicas:    1,
		Upstream:    pending,
	}
	_, err := f.snapshot(t).Place(placementFor(t, inference), testStart)
	expectReason(t, err, "gpu_reserved_before_search")

	// The rule is enforced independently of the expense-rank comparison, so it
	// still holds for a model slot that asks for no accelerator of its own.
	expectReason(t,
		EnforceGPUReservationRule(PoolGPUInference, AcceleratorNone, pending),
		"gpu_reserved_before_search")

	// And it applies to any accelerated pool, not only inference.
	expectReason(t,
		EnforceGPUReservationRule(PoolGPUTraining, AcceleratorB200, pending),
		"gpu_reserved_before_search")

	// Search itself is a separately scheduled CPU/high-memory resource class
	// and is not blocked by its own pending stage.
	if err := EnforceGPUReservationRule(PoolSearchCPU, AcceleratorNone, pending); err != nil {
		t.Fatalf("CPU search must not be blocked by the GPU reservation rule: %v", err)
	}

	// Completed search releases the GPU.
	inference.Upstream = []UpstreamResourceStage{upstream(t, orchestration.StageReferenceBuild, true)}
	if _, err := f.snapshot(t).Place(placementFor(t, inference), testStart); err != nil {
		t.Fatalf("completed reference search must release the GPU placement: %v", err)
	}
}

// Holding a cheap downstream resource while an expensive upstream class runs
// wastes nothing worth protecting, so it is allowed.
func TestPlacementAllowsCheaperDownstreamWhileUpstreamIncomplete(t *testing.T) {
	f := newFixture(t)
	transfer := AdmissionRequest{
		WorkloadID:  mustID(t, "workload", testStart),
		Tenant:      "research",
		Workspace:   "research-team",
		StageKind:   orchestration.StageArtifactTransfer,
		Pool:        PoolArtifactDataPlane,
		Accelerator: AcceleratorNone,
		Priority:    PriorityBatch,
		Demand:      cpuDemand(1_000, gibibyte, 2*gibibyte, 1),
		Replicas:    1,
		Upstream:    []UpstreamResourceStage{upstream(t, orchestration.StageTraining, false)},
	}
	if _, err := f.snapshot(t).Place(placementFor(t, transfer), testStart); err != nil {
		t.Fatalf("a cheaper downstream class must not be blocked: %v", err)
	}
}

// Sequencing inside one resource class is ordinary orchestration, not the
// cross-class hold the invariant forbids.
func TestPlacementAllowsIncompleteUpstreamInTheSameResourceClass(t *testing.T) {
	f := newFixture(t)
	request := cpuRequest(t, "research")
	request.Upstream = []UpstreamResourceStage{upstream(t, orchestration.StageCurate, false)}
	if _, err := f.snapshot(t).Place(placementFor(t, request), testStart); err != nil {
		t.Fatalf("same-class sequencing must not be blocked: %v", err)
	}
}

func TestPlacementRejectsTopologyOnANonTopologyFlavor(t *testing.T) {
	f := newFixture(t)
	request := cpuRequest(t, "research")
	request.Topology = RequireTopology(TopologyLevelHost)
	_, err := f.snapshot(t).Place(placementFor(t, request), testStart)
	expectReason(t, err, "topology_flavor_unsupported")
}

func TestPlacementRequestValidationRejectsMalformedFields(t *testing.T) {
	mutators := map[string]func(*PlacementRequest){
		"run_id_invalid":   func(r *PlacementRequest) { r.RunID = "run-1" },
		"stage_id_invalid": func(r *PlacementRequest) { r.StageID = "" },
		"attempt_invalid":  func(r *PlacementRequest) { r.Attempt = 0 },
		"upstream_self_reference": func(r *PlacementRequest) {
			r.Admission.Upstream = []UpstreamResourceStage{{
				StageID: r.StageID,
				Kind:    orchestration.StageCurate,
			}}
		},
	}
	for reason, mutate := range mutators {
		t.Run(reason, func(t *testing.T) {
			request := placementFor(t, cpuRequest(t, "research"))
			mutate(&request)
			expectReason(t, request.Validate(), reason)
		})
	}
}

func TestUpstreamListRejectsDuplicatesAndUnknownStages(t *testing.T) {
	stage := upstream(t, orchestration.StageCurate, false)
	expectReason(t, validateUpstream([]UpstreamResourceStage{stage, stage}), "upstream_stage_duplicate")
	expectReason(t,
		validateUpstream([]UpstreamResourceStage{{StageID: "not-an-id", Kind: orchestration.StageCurate}}),
		"upstream_stage_id_invalid")
	expectReason(t,
		validateUpstream([]UpstreamResourceStage{{StageID: stage.StageID, Kind: orchestration.StageKind("teleport")}}),
		"upstream_stage_kind_invalid")
	oversized := make([]UpstreamResourceStage, MaximumUpstreamStages+1)
	expectReason(t, validateUpstream(oversized), "upstream_stage_bound")
}

func TestPlacementValidationRejectsTamperedFields(t *testing.T) {
	f := newFixture(t)
	placement, err := f.snapshot(t).Place(placementFor(t, cpuRequest(t, "research")), testStart)
	if err != nil {
		t.Fatal(err)
	}
	mutators := map[string]func(*Placement){
		"placement_digest_mismatch":          func(p *Placement) { p.Tenant = "platform" },
		"placement_total_mismatch":           func(p *Placement) { p.Replicas = 3 },
		"placement_topology_digest_mismatch": func(p *Placement) { p.TopologyDigest = placement.Digest },
		"pool_binding_inconsistent":          func(p *Placement) { p.Pool.Flavor = FlavorH100 },
	}
	for reason, mutate := range mutators {
		t.Run(reason, func(t *testing.T) {
			tampered := placement.clone()
			mutate(&tampered)
			expectReason(t, tampered.Validate(), reason)
		})
	}
	if _, err := QueueLabels(Placement{}); err == nil {
		t.Fatal("the zero placement must not produce queue labels")
	}
}
