// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import (
	"context"
	"testing"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

var testStart = time.Date(2026, time.August, 23, 6, 0, 0, 0, time.UTC)

const (
	gibibyte = uint64(1) << 30
	tebibyte = uint64(1) << 40
)

func mustID(t *testing.T, kind string, at time.Time) identifiers.ID {
	t.Helper()
	id, err := identifiers.NewIDAt(identifiers.MustParseKind(kind), at)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustDomain(t *testing.T, class WorkloadClass) CapacityDomain {
	t.Helper()
	domain, err := DomainFor(class)
	if err != nil {
		t.Fatal(err)
	}
	return domain
}

func cpuDemand(cpu, memory, storage, pods uint64) Demand {
	return Demand{
		ResourceCPU:              cpu,
		ResourceMemory:           memory,
		ResourceEphemeralStorage: storage,
		ResourcePods:             pods,
	}
}

func gpuDemand(cpu, memory, storage, gpu, pods uint64) Demand {
	demand := cpuDemand(cpu, memory, storage, pods)
	demand[ResourceGPU] = gpu
	return demand
}

type fixture struct {
	clock      *clock.FakeClock
	repository *MemoryRepository
	service    Service
	batch      CapacityDomain
	training   CapacityDomain
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	fake := clock.NewFake(testStart)
	repository := NewMemoryRepository(64)
	ctx := context.Background()
	batch := mustDomain(t, WorkloadClassBatchCPU)
	training := mustDomain(t, WorkloadClassTrainingH100)
	if err := repository.PutQuota(ctx, batch, cpuDemand(64_000, 256*gibibyte, tebibyte, 128)); err != nil {
		t.Fatal(err)
	}
	if err := repository.PutQuota(ctx, training, gpuDemand(64_000, 512*gibibyte, tebibyte, 8, 32)); err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []string{"research", "platform"} {
		if err := repository.PutWeight(ctx, tenant, 1); err != nil {
			t.Fatal(err)
		}
	}
	return fixture{
		clock:      fake,
		repository: repository,
		service:    Service{Repository: repository, Clock: fake, ReservationTTL: time.Minute, Fence: 7},
		batch:      batch,
		training:   training,
	}
}

func (f fixture) snapshot(t *testing.T) FleetSnapshot {
	t.Helper()
	snapshot, err := f.repository.Snapshot(context.Background(), f.clock.Now().Round(0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

// cpuRequest is a featurization-CPU admission for one tenant. Two replicas of
// one pod each, so the total is exactly twice the per-replica demand.
func cpuRequest(t *testing.T, tenant string) AdmissionRequest {
	t.Helper()
	return AdmissionRequest{
		WorkloadID:  mustID(t, "workload", testStart),
		Tenant:      tenant,
		Workspace:   "research-team",
		StageKind:   orchestration.StagePreprocess,
		Pool:        PoolFeaturizationCPU,
		Accelerator: AcceleratorNone,
		Priority:    PriorityBatch,
		Demand:      cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1),
		Replicas:    2,
	}
}

func trainingRequest(t *testing.T, tenant string) AdmissionRequest {
	t.Helper()
	return AdmissionRequest{
		WorkloadID:  mustID(t, "workload", testStart),
		Tenant:      tenant,
		Workspace:   "research-team",
		StageKind:   orchestration.StageTraining,
		Pool:        PoolGPUTraining,
		Accelerator: AcceleratorH100,
		Priority:    PriorityPlatformCritical,
		Demand:      gpuDemand(8_000, 64*gibibyte, 128*gibibyte, 1, 1),
		Replicas:    2,
		Topology:    RequireTopology(TopologyLevelZone),
	}
}

func placementFor(t *testing.T, request AdmissionRequest) PlacementRequest {
	t.Helper()
	return PlacementRequest{
		Admission: request,
		RunID:     mustID(t, "run", testStart).String(),
		StageID:   mustID(t, "stage", testStart).String(),
		Attempt:   1,
	}
}

func expectReason(t *testing.T, err error, reason string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected fault %q, got nil", reason)
	}
	if got := faults.ReasonOf(err); got != reason {
		t.Fatalf("expected fault reason %q, got %q (%v)", reason, got, err)
	}
}

func TestAdmitAcceptsAWellFormedCPURequest(t *testing.T) {
	f := newFixture(t)
	verdict, err := f.snapshot(t).Admit(cpuRequest(t, "research"), testStart)
	if err != nil {
		t.Fatal(err)
	}
	if !verdict.Admitted {
		t.Fatal("expected an admitted verdict")
	}
	if verdict.Pool.Domain != f.batch {
		t.Fatalf("expected the batch-cpu domain, got %s", verdict.Pool.Domain)
	}
	if verdict.Pool.Flavor != FlavorCPUGeneralOnDemand {
		t.Fatalf("expected the CPU flavor, got %s", verdict.Pool.Flavor)
	}
	if verdict.Total[ResourceCPU] != 4_000 || verdict.Total[ResourcePods] != 2 {
		t.Fatalf("expected the per-replica demand scaled by two, got %v", verdict.Total)
	}
}

// A GPU demand in the batch-cpu domain is denied rather than queued: the
// batch-cpu ClusterQueue does not list nvidia.com/gpu among its covered
// resources, so no amount of waiting would ever admit it.
func TestAdmitDeniesGPUDemandInTheBatchDomain(t *testing.T) {
	f := newFixture(t)
	request := cpuRequest(t, "research")
	request.Demand[ResourceGPU] = 1
	_, err := f.snapshot(t).Admit(request, testStart)
	expectReason(t, err, "demand_resource_uncovered")
}

func TestAdmitRequiresThePoolDeclaredByTheStageKind(t *testing.T) {
	f := newFixture(t)
	request := cpuRequest(t, "research")
	request.Pool = PoolGPUTraining
	_, err := f.snapshot(t).Admit(request, testStart)
	expectReason(t, err, "pool_stage_mismatch")
}

func TestAdmitRefusesAHeldZeroQuotaDomain(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// Reset the batch domain to the state every ClusterQueue ships in.
	if err := f.repository.PutQuota(ctx, f.batch, cpuDemand(0, 0, 0, 0)); err != nil {
		t.Fatal(err)
	}
	_, err := f.snapshot(t).Admit(cpuRequest(t, "research"), testStart)
	expectReason(t, err, "capacity_domain_inactive")
}

func TestAdmitRefusesAnUnobservedDomain(t *testing.T) {
	f := newFixture(t)
	snapshot := f.snapshot(t)
	kept := make([]Allocatable, 0, len(snapshot.Allocatables))
	for _, allocatable := range snapshot.Allocatables {
		if allocatable.Domain != f.batch {
			kept = append(kept, allocatable)
		}
	}
	snapshot.Allocatables = kept
	_, err := snapshot.Admit(cpuRequest(t, "research"), testStart)
	expectReason(t, err, "capacity_domain_unobserved")
}

func TestAdmitRefusesWhenCapacityIsExhausted(t *testing.T) {
	f := newFixture(t)
	request := cpuRequest(t, "research")
	request.Demand = cpuDemand(60_000, 200*gibibyte, 512*gibibyte, 1)
	request.Replicas = 2
	_, err := f.snapshot(t).Admit(request, testStart)
	expectReason(t, err, "capacity_exhausted")
}

func TestAdmitRefusesOverShareUseWhileAPeerIsStarved(t *testing.T) {
	f := newFixture(t)
	snapshot := f.snapshot(t)
	// Two tenants of equal weight split 64 cores, so 32 is the entitlement.
	// "research" is asking for 40 while "platform" holds nothing at all.
	request := cpuRequest(t, "research")
	request.Demand = cpuDemand(20_000, 8*gibibyte, 16*gibibyte, 1)
	request.Replicas = 2
	_, err := snapshot.Admit(request, testStart)
	expectReason(t, err, "fair_share_exhausted")
}

func TestAdmitAllowsOverShareUseWhenNoPeerIsStarved(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	solo := NewMemoryRepository(8)
	if err := solo.PutQuota(ctx, f.batch, cpuDemand(64_000, 256*gibibyte, tebibyte, 128)); err != nil {
		t.Fatal(err)
	}
	if err := solo.PutWeight(ctx, "research", 1); err != nil {
		t.Fatal(err)
	}
	snapshot, err := solo.Snapshot(ctx, testStart)
	if err != nil {
		t.Fatal(err)
	}
	request := cpuRequest(t, "research")
	request.Demand = cpuDemand(20_000, 8*gibibyte, 16*gibibyte, 1)
	request.Replicas = 2
	verdict, err := snapshot.Admit(request, testStart)
	if err != nil {
		t.Fatal(err)
	}
	if !verdict.Admitted {
		t.Fatal("a sole tenant should be able to borrow idle capacity")
	}
}

func TestAdmitIsDeterministicForTheSameSnapshot(t *testing.T) {
	f := newFixture(t)
	snapshot := f.snapshot(t)
	request := cpuRequest(t, "research")
	first, err := snapshot.Admit(request, testStart)
	if err != nil {
		t.Fatal(err)
	}
	second, err := snapshot.Admit(request, testStart)
	if err != nil {
		t.Fatal(err)
	}
	if !first.SnapshotDigest.Equal(second.SnapshotDigest) {
		t.Fatal("the same snapshot produced two different fingerprints")
	}
	if first.QueuePriority != second.QueuePriority || first.FairSharePosition != second.FairSharePosition {
		t.Fatalf("admission is not deterministic: %+v vs %+v", first, second)
	}
}

func TestAdmitRejectsASnapshotFromTheFuture(t *testing.T) {
	f := newFixture(t)
	snapshot := f.snapshot(t)
	_, err := snapshot.Admit(cpuRequest(t, "research"), testStart.Add(-time.Minute))
	expectReason(t, err, "snapshot_from_the_future")
}

func TestAdmitRejectsASnapshotFromAReplacedTopology(t *testing.T) {
	f := newFixture(t)
	snapshot := f.snapshot(t)
	snapshot.TopologyDigest = identifiers.SHA256String("a different topology")
	_, err := snapshot.Admit(cpuRequest(t, "research"), testStart)
	expectReason(t, err, "snapshot_topology_mismatch")
}

func TestAdmissionRequestValidationRejectsMalformedFields(t *testing.T) {
	mutators := map[string]func(*AdmissionRequest){
		"workload_id_invalid":    func(r *AdmissionRequest) { r.WorkloadID = identifiers.ID{} },
		"tenant_invalid":         func(r *AdmissionRequest) { r.Tenant = "Research Team" },
		"workspace_invalid":      func(r *AdmissionRequest) { r.Workspace = "" },
		"stage_kind_invalid":     func(r *AdmissionRequest) { r.StageKind = orchestration.StageKind("teleport") },
		"priority_class_invalid": func(r *AdmissionRequest) { r.Priority = PriorityClass("urgent") },
		"replicas_out_of_range":  func(r *AdmissionRequest) { r.Replicas = 0 },
		"demand_empty":           func(r *AdmissionRequest) { r.Demand = Demand{} },
		"demand_pods_required":   func(r *AdmissionRequest) { delete(r.Demand, ResourcePods) },
		"accelerator_invalid":    func(r *AdmissionRequest) { r.Accelerator = Accelerator("tpu") },
		"topology_constraint_ambiguous": func(r *AdmissionRequest) {
			r.Topology = TopologyConstraint{Required: TopologyLevelZone, Preferred: TopologyLevelHost}
		},
	}
	for reason, mutate := range mutators {
		t.Run(reason, func(t *testing.T) {
			request := cpuRequest(t, "research")
			mutate(&request)
			expectReason(t, request.Validate(), reason)
		})
	}
}

// TestServiceRequiresProviders nils each collaborator in turn. A scheduling
// service that writes without a repository, without a usable clock, or without
// a leadership fence is granting fleet-wide capacity it cannot prove it owns.
func TestServiceRequiresProviders(t *testing.T) {
	f := newFixture(t)
	request := placementFor(t, cpuRequest(t, "research"))
	ctx := context.Background()

	if _, err := (Service{}).Place(ctx, request); err == nil {
		t.Fatal("expected the zero service to refuse a placement")
	} else {
		expectReason(t, err, "repository_unavailable")
	}

	missingRepository := f.service
	missingRepository.Repository = nil
	if _, err := missingRepository.Place(ctx, request); err == nil {
		t.Fatal("expected a nil repository to be refused")
	} else {
		expectReason(t, err, "repository_unavailable")
	}

	typedNilRepository := f.service
	typedNilRepository.Repository = (*MemoryRepository)(nil)
	if _, err := typedNilRepository.Place(ctx, request); err == nil {
		t.Fatal("expected a typed-nil repository to be refused")
	} else {
		expectReason(t, err, "repository_unavailable")
	}

	typedNilClock := f.service
	typedNilClock.Clock = (*clock.FakeClock)(nil)
	if _, err := typedNilClock.Place(ctx, request); err == nil {
		t.Fatal("expected a typed-nil clock to be refused")
	} else {
		expectReason(t, err, "clock_unavailable")
	}

	unfenced := f.service
	unfenced.Fence = 0
	if _, err := unfenced.Place(ctx, request); err == nil {
		t.Fatal("expected an unfenced service to be refused")
	} else {
		expectReason(t, err, "leadership_fence_missing")
	}

	//nolint:staticcheck // the nil context is the input under test
	if _, err := f.service.Place(nil, request); err == nil {
		t.Fatal("expected a nil context to be refused")
	} else {
		expectReason(t, err, "context_nil")
	}
}

func TestServicePlaceRecordsAReservationAndReplaysIt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	request := placementFor(t, cpuRequest(t, "research"))
	first, err := f.service.Place(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed {
		t.Fatal("the first placement is not a replay")
	}
	if first.Reservation.State != ReservationHeld {
		t.Fatalf("expected a held reservation, got %s", first.Reservation.State)
	}
	second, err := f.service.Place(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed {
		t.Fatal("the same run, stage, and attempt must replay the original reservation")
	}
	if second.Reservation.ID != first.Reservation.ID {
		t.Fatal("a replay returned a different reservation")
	}
}
