// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package schedulingtest

import (
	"context"
	"testing"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/scheduling"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

// DefaultLeaseFence is the leadership fence every fixture write carries.
//
// It is well above one so a case can mint a strictly older fence without
// reaching zero. Zero is rejected as lease_fence_invalid before the store's own
// fence is ever consulted, so a fixture that reached it would prove the wrong
// thing.
const DefaultLeaseFence = uint64(9)

const (
	// The two fixture tenants. They are recorded in the reverse of their sorted
	// order everywhere the suite records both, so an adapter that returns
	// fair-share claims in insertion order -- or lets a database collation
	// decide -- fails the ordering assertions instead of forking a fingerprint
	// in production.
	tenantAlpha = "alpha-lab"
	tenantZeta  = "zeta-lab"

	gibibyte = uint64(1) << 30
	tebibyte = uint64(1) << 40
)

// start anchors every fixture. A fixed instant with no sub-second component,
// because a reservation's version digest seals its timestamps and the suite
// compares those digests across a JSON and SQL round trip.
var start = time.Date(2026, time.August, 23, 6, 0, 0, 0, time.UTC)

// Fleet is the fleet-configuration surface both adapters expose beside
// Repository.
//
// Recording a quota and a weight is not part of the durable seam -- placement
// never writes one -- but a repository that has neither admits nothing, so a
// conformance suite cannot reach a single interesting case without them. The
// suite therefore asks for this wider type at runtime and reports plainly when
// an adapter does not have it, rather than skipping the fleet-shaped half of
// the contract.
type Fleet interface {
	scheduling.Repository
	PutQuota(context.Context, scheduling.CapacityDomain, scheduling.Demand) error
	PutWeight(context.Context, string, uint32) error
}

// Factory builds the repository under test. It is called once per conformance
// case, and each call must yield a repository holding nothing: several cases
// assert on an epoch that only advances, on a store-wide fence floor, and on a
// snapshot that names exactly the domains someone recorded.
type Factory func(testing.TB) scheduling.Repository

// Conformance runs every case both adapters owe.
func Conformance(t *testing.T, factory Factory) {
	t.Helper()
	if factory == nil {
		t.Fatal("schedulingtest.Conformance requires a factory")
	}
	t.Run("UnconfiguredFleet", func(t *testing.T) { unconfiguredFleet(t, factory) })
	t.Run("FleetConfiguration", func(t *testing.T) { fleetConfiguration(t, factory) })
	t.Run("SnapshotAcrossCapacityDomains", func(t *testing.T) { snapshotAcrossCapacityDomains(t, factory) })
	t.Run("UsageWithoutAFairShareClaim", func(t *testing.T) { usageWithoutAFairShareClaim(t, factory) })
	t.Run("ReplayByPlacementKey", func(t *testing.T) { replayByPlacementKey(t, factory) })
	t.Run("PlacementKeyReusedVersusTerminal", func(t *testing.T) { placementKeyReusedVersusTerminal(t, factory) })
	t.Run("LedgerRecheckedInsideTheWrite", func(t *testing.T) { ledgerRecheckedInsideTheWrite(t, factory) })
	t.Run("FenceMonotonicity", func(t *testing.T) { fenceMonotonicity(t, factory) })
	t.Run("SnapshotStaleness", func(t *testing.T) { snapshotStaleness(t, factory) })
	t.Run("TransitionPreconditions", func(t *testing.T) { transitionPreconditions(t, factory) })
	t.Run("ExpiryIsAWrite", func(t *testing.T) { expiryIsAWrite(t, factory) })
	t.Run("WholePlanPreemption", func(t *testing.T) { wholePlanPreemption(t, factory) })
	t.Run("ArgumentValidation", func(t *testing.T) { argumentValidation(t, factory) })
}

// unconfiguredFleet pins the difference between a queue that is held and a
// queue nobody measured.
//
// A fresh store has recorded no quota, so its snapshot must carry no ledger at
// all. An adapter that projected the three compiled-in capacity domains into
// zero ledgers would pass every other case here and let placement admit against
// capacity nobody observed -- which is the fail-open the domain exists to
// prevent, and the one an outer join makes easy to write.
func unconfiguredFleet(t *testing.T, factory Factory) {
	t.Helper()
	rig := newRig(t, factory)
	batch := domainFor(t, scheduling.WorkloadClassBatchCPU)

	snapshot := rig.snapshot(start)
	if len(snapshot.Allocatables) != 0 || len(snapshot.Shares) != 0 {
		t.Fatalf("an unconfigured fleet reported %d capacity ledgers and %d fair-share views, want none",
			len(snapshot.Allocatables), len(snapshot.Shares))
	}
	if !snapshot.ObservedAt.Equal(start) {
		t.Fatalf("snapshot observed at %s, want the transaction time %s", snapshot.ObservedAt, start)
	}
	if !snapshot.TopologyDigest.Equal(scheduling.TopologyFingerprint()) {
		t.Fatal("snapshot does not name the compiled topology contract")
	}
	if snapshot.Epoch == 0 {
		t.Fatal("snapshot carries no epoch; a zero epoch is not a decidable fleet state")
	}

	if active := rig.held(batch, start); len(active) != 0 {
		t.Fatalf("an unconfigured fleet holds %d reservations, want none", len(active))
	}
	_, err := rig.repository.Get(context.Background(), newID(t, "reservation"))
	requireFault(t, err, faults.CodeNotFound, "reservation_not_found", "Get of an unrecorded reservation")
}

// fleetConfiguration covers the recorded-configuration rules the two adapters
// state independently: one keeps a map, the other keeps two tables with their
// own bounds, and both are supposed to raise the same faults.
func fleetConfiguration(t *testing.T, factory Factory) {
	t.Helper()

	t.Run("ZeroQuotaIsARecordedState", func(t *testing.T) {
		rig := newRig(t, factory)
		batch := domainFor(t, scheduling.WorkloadClassBatchCPU)
		rig.putQuota(batch, scheduling.Demand{})

		// Recorded and empty, not absent. The queue is held rather than
		// unmeasured, and the snapshot has to be able to say which.
		snapshot := rig.snapshot(start)
		if len(snapshot.Allocatables) != 1 {
			t.Fatalf("recording a zero quota produced %d ledgers, want 1", len(snapshot.Allocatables))
		}
		if !snapshot.Allocatables[0].Nominal.IsZero() {
			t.Fatalf("recorded zero quota came back as %v", snapshot.Allocatables[0].Nominal)
		}
	})

	t.Run("WeightsAreValidatedBeforeTheyAreRecorded", func(t *testing.T) {
		rig := newRig(t, factory)
		ctx := context.Background()

		err := rig.fleet.PutWeight(ctx, tenantAlpha, 0)
		requireFault(t, err, faults.CodeInvalidArgument, "share_weight_out_of_range", "PutWeight with a zero weight")
		err = rig.fleet.PutWeight(ctx, tenantAlpha, scheduling.MaximumShareWeight+1)
		requireFault(t, err, faults.CodeInvalidArgument, "share_weight_out_of_range", "PutWeight past the weight bound")
		err = rig.fleet.PutWeight(ctx, "", 1)
		requireFault(t, err, faults.CodeInvalidArgument, "tenant_invalid", "PutWeight with an empty tenant")
		// The alphabet is the Kubernetes label-value shape, because the tenant
		// name is projected onto an object the API server validates.
		err = rig.fleet.PutWeight(ctx, "Alpha Lab", 1)
		requireFault(t, err, faults.CodeInvalidArgument, "tenant_invalid", "PutWeight with a non-label tenant")
	})

	t.Run("QuotaMayNotBeReducedBelowHeldCapacity", func(t *testing.T) {
		rig := newRig(t, factory)
		batch := domainFor(t, scheduling.WorkloadClassBatchCPU)
		rig.seedBatch()
		spec := batchSpec(t, tenantAlpha, start)
		rig.reserve(spec, DefaultLeaseFence, scheduling.DefaultReservationTTL)
		held := spec.total(t)

		// Exactly what is held is a legal quota: the ledger is full, not
		// over-committed.
		rig.putQuota(batch, held)

		// One millicore below is not. There is no transition that could repair
		// an over-reserved ledger, so every later snapshot would fail to
		// validate -- the reduction has to be refused at the write.
		short := held.Clone()
		short[scheduling.ResourceCPU]--
		err := rig.fleet.PutQuota(context.Background(), batch, short)
		requireFault(t, err, faults.CodeFailedPrecondition, "quota_below_reserved",
			"PutQuota below the held capacity")
	})
}

// snapshotAcrossCapacityDomains is the fingerprint-parity case, and it covers
// two capacity domains and two tenants on purpose.
//
// Reserve compares a caller's decision against a snapshot the store rebuilds
// inside the write, and the comparison is a digest. Everything that feeds that
// digest -- which domains appear, which tenants carry a claim, whose usage is
// charged where -- has to agree between the two adapters exactly, not
// approximately. A single-domain fixture cannot see any of the collation sorts
// that exist to keep them agreeing, because with one domain and one tenant
// every ordering is the same ordering.
func snapshotAcrossCapacityDomains(t *testing.T, factory Factory) {
	t.Helper()
	rig := newRig(t, factory)
	batch := domainFor(t, scheduling.WorkloadClassBatchCPU)
	training := domainFor(t, scheduling.WorkloadClassTrainingH100)

	// Recorded in the reverse of their canonical order. Every ordering
	// assertion below is therefore a real question.
	rig.putQuota(training, trainingQuota())
	rig.putQuota(batch, batchQuota())
	rig.putWeight(tenantZeta, 1)
	rig.putWeight(tenantAlpha, 1)

	baseline := rig.snapshot(start).Epoch

	firstBatch := rig.reserve(batchSpec(t, tenantAlpha, start), DefaultLeaseFence, scheduling.DefaultReservationTTL)
	secondBatch := rig.reserve(batchSpec(t, tenantAlpha, start), DefaultLeaseFence, scheduling.DefaultReservationTTL)
	gpu := rig.reserve(trainingSpec(t, tenantZeta, start), DefaultLeaseFence, scheduling.DefaultReservationTTL)

	observed := start.Add(time.Minute)
	snapshot := rig.snapshot(observed)

	if snapshot.Epoch <= baseline {
		t.Fatalf("epoch %d did not advance past %d across three reservations", snapshot.Epoch, baseline)
	}
	if len(snapshot.Allocatables) != 2 || len(snapshot.Shares) != 2 {
		t.Fatalf("snapshot carries %d ledgers and %d fair-share views, want 2 and 2",
			len(snapshot.Allocatables), len(snapshot.Shares))
	}
	// Ascending by the domain's own canonical triple, which is what the
	// reference adapter sorts on. A server-side ORDER BY would order by the
	// database collation, and a collation is free to disagree with byte order
	// about the hyphens these names contain.
	if snapshot.Allocatables[0].Domain != batch || snapshot.Allocatables[1].Domain != training {
		t.Fatalf("capacity ledgers are ordered %q then %q, want ascending by domain",
			snapshot.Allocatables[0].Domain.String(), snapshot.Allocatables[1].Domain.String())
	}
	if snapshot.Shares[0].Domain != batch || snapshot.Shares[1].Domain != training {
		t.Fatalf("fair-share views are ordered %q then %q, want ascending by domain",
			snapshot.Shares[0].Domain.String(), snapshot.Shares[1].Domain.String())
	}

	batchUsed := sumDemand(firstBatch.Placement.Total, secondBatch.Placement.Total)
	gpuUsed := gpu.Placement.Total
	requireDemand(t, snapshot.Allocatables[0].Reserved, batchUsed, "batch-cpu reserved capacity")
	requireDemand(t, snapshot.Allocatables[1].Reserved, gpuUsed, "training-h100 reserved capacity")
	requireDemand(t, snapshot.Allocatables[0].Nominal, batchQuota(), "batch-cpu nominal quota")
	requireDemand(t, snapshot.Allocatables[1].Nominal, trainingQuota(), "training-h100 nominal quota")

	// One claim per WEIGHTED tenant, in every domain, sorted by tenant, present
	// even at zero usage. Building the claim set from usage instead would drop
	// the idle tenant, change the fingerprint, and make every decision taken
	// against the other adapter stale here.
	for index, share := range snapshot.Shares {
		if len(share.Claims) != 2 {
			t.Fatalf("%s carries %d fair-share claims, want one per weighted tenant",
				share.Domain.WorkloadClass(), len(share.Claims))
		}
		if share.Claims[0].Tenant != tenantAlpha || share.Claims[1].Tenant != tenantZeta {
			t.Fatalf("%s claims are ordered %q then %q, want ascending by tenant",
				share.Domain.WorkloadClass(), share.Claims[0].Tenant, share.Claims[1].Tenant)
		}
		alpha, zeta := share.Claims[0].Used, share.Claims[1].Used
		if index == 0 {
			requireDemand(t, alpha, batchUsed, "batch-cpu usage charged to "+tenantAlpha)
			requireDemand(t, zeta, scheduling.Demand{}, "batch-cpu usage charged to "+tenantZeta)
			continue
		}
		requireDemand(t, alpha, scheduling.Demand{}, "training-h100 usage charged to "+tenantAlpha)
		requireDemand(t, zeta, gpuUsed, "training-h100 usage charged to "+tenantZeta)
	}

	// The parity assertion itself. `want` is assembled here, from values, with
	// no adapter involved: an adapter that agrees with it agrees with every
	// other adapter that agrees with it, which is the only way one suite run
	// against one adapter can prove a cross-adapter property. Epoch is copied
	// across because Validate requires a non-zero one and the fingerprint does
	// not cover it -- a snapshot is stale when its ledgers moved, not when
	// someone else's write minted a counter.
	want := scheduling.FleetSnapshot{
		Epoch:      snapshot.Epoch,
		ObservedAt: observed,
		Allocatables: []scheduling.Allocatable{
			{Domain: batch, Nominal: batchQuota(), Reserved: batchUsed.Clone()},
			{Domain: training, Nominal: trainingQuota(), Reserved: gpuUsed.Clone()},
		},
		Shares: []scheduling.FairShare{
			{Domain: batch, Capacity: batchQuota(), Claims: []scheduling.ShareClaim{
				{Tenant: tenantAlpha, Weight: 1, Used: batchUsed.Clone()},
				{Tenant: tenantZeta, Weight: 1, Used: make(scheduling.Demand)},
			}},
			{Domain: training, Capacity: trainingQuota(), Claims: []scheduling.ShareClaim{
				{Tenant: tenantAlpha, Weight: 1, Used: make(scheduling.Demand)},
				{Tenant: tenantZeta, Weight: 1, Used: gpuUsed.Clone()},
			}},
		},
		TopologyDigest: scheduling.TopologyFingerprint(),
	}
	if err := want.Validate(); err != nil {
		t.Fatalf("the expected fleet snapshot is not valid: %v", err)
	}
	if !snapshot.Fingerprint().Equal(want.Fingerprint()) {
		t.Fatalf("fleet fingerprint = %s, want %s\n got: %#v\nwant: %#v",
			snapshot.Fingerprint(), want.Fingerprint(), snapshot, want)
	}

	// Held is per domain, and ordered on the identifier's own bytes.
	batchHeld := rig.held(batch, observed)
	if len(batchHeld) != 2 {
		t.Fatalf("batch-cpu holds %d reservations, want 2", len(batchHeld))
	}
	if batchHeld[0].ID.String() > batchHeld[1].ID.String() {
		t.Fatalf("held reservations are ordered %q then %q, want ascending by identifier",
			batchHeld[0].ID, batchHeld[1].ID)
	}
	trainingHeld := rig.held(training, observed)
	if len(trainingHeld) != 1 || trainingHeld[0].ID != gpu.ID {
		t.Fatalf("training-h100 holds %#v, want only %q", trainingHeld, gpu.ID)
	}
}

// usageWithoutAFairShareClaim pins the claim-set rule from its other side.
//
// A tenant holding capacity with no recorded weight is counted in Reserved --
// it really is holding the capacity -- and is absent from Claims, because it
// has no fair-share claim to rank. The two adapters reach those two numbers by
// different routes: one walks its reservation map twice, the other sums a
// column group and joins the weight table. That join is exactly where a tenant
// with usage and no weight goes missing from Reserved, or turns up in Claims
// under a weight nobody recorded, and neither mistake is visible from a fixture
// where every tenant is weighted.
func usageWithoutAFairShareClaim(t *testing.T, factory Factory) {
	t.Helper()
	rig := newRig(t, factory)
	batch := domainFor(t, scheduling.WorkloadClassBatchCPU)

	// No weight is recorded yet, so fairness constrains nothing and an
	// unweighted tenant is admissible at all. It stops being admissible the
	// moment a peer records a weight, which is why the order here matters.
	rig.putQuota(batch, batchQuota())
	unweighted := rig.reserve(batchSpec(t, tenantAlpha, start), DefaultLeaseFence, scheduling.DefaultReservationTTL)
	rig.putWeight(tenantZeta, 1)

	observed := start.Add(time.Minute)
	snapshot := rig.snapshot(observed)
	if len(snapshot.Allocatables) != 1 || len(snapshot.Shares) != 1 || len(snapshot.Shares[0].Claims) != 1 {
		t.Fatalf("snapshot carries %d ledgers and %#v, want one ledger and one claim",
			len(snapshot.Allocatables), snapshot.Shares)
	}
	if claim := snapshot.Shares[0].Claims[0]; claim.Tenant != tenantZeta {
		t.Fatalf("fair-share claim names %q, want only the weighted tenant %q", claim.Tenant, tenantZeta)
	}
	requireDemand(t, snapshot.Shares[0].Claims[0].Used, scheduling.Demand{},
		"the weighted tenant's recorded usage")
	requireDemand(t, snapshot.Allocatables[0].Reserved, unweighted.Placement.Total,
		"capacity reserved by an unweighted tenant")

	want := scheduling.FleetSnapshot{
		Epoch:      snapshot.Epoch,
		ObservedAt: observed,
		Allocatables: []scheduling.Allocatable{
			{Domain: batch, Nominal: batchQuota(), Reserved: unweighted.Placement.Total.Clone()},
		},
		Shares: []scheduling.FairShare{
			{Domain: batch, Capacity: batchQuota(), Claims: []scheduling.ShareClaim{
				{Tenant: tenantZeta, Weight: 1, Used: make(scheduling.Demand)},
			}},
		},
		TopologyDigest: scheduling.TopologyFingerprint(),
	}
	if err := want.Validate(); err != nil {
		t.Fatalf("the expected fleet snapshot is not valid: %v", err)
	}
	if !snapshot.Fingerprint().Equal(want.Fingerprint()) {
		t.Fatalf("fleet fingerprint = %s, want %s", snapshot.Fingerprint(), want.Fingerprint())
	}

	// The hold is still a hold: it occupies capacity and is still preemptable,
	// whatever the fair-share view has to say about its tenant.
	if active := rig.held(batch, observed); len(active) != 1 || active[0].ID != unweighted.ID {
		t.Fatalf("an unweighted tenant's hold is not reported as held: %#v", active)
	}
}

// replayByPlacementKey is the property that makes an at-least-once placement
// queue safe to run: the second delivery of one work item must return the hold
// the first one took, not take a second one.
//
// A retry does not look like the original. It mints a fresh reservation ID and
// decides at a fresh instant, so nothing but the run, stage, and attempt
// coordinates connects the two -- which is exactly what PlacementKey is.
func replayByPlacementKey(t *testing.T, factory Factory) {
	t.Helper()
	rig := newRig(t, factory)
	ctx := context.Background()
	batch := domainFor(t, scheduling.WorkloadClassBatchCPU)
	rig.seedBatch()

	spec := batchSpec(t, tenantAlpha, start)
	first := rig.reserve(spec, DefaultLeaseFence, scheduling.DefaultReservationTTL)
	charged := rig.reserved(batch, start)

	// What Reserve answered with is what the store recorded. Reserve returns
	// the candidate it was handed rather than a re-read of the row it wrote, so
	// nothing else here would notice a durable write that reshaped the record
	// on its way to disk.
	stored := rig.get(first.ID)
	if stored.Version.String() != first.Version.String() || stored.State != first.State ||
		stored.Sequence != first.Sequence || stored.LeaseFence != first.LeaseFence ||
		!stored.CreatedAt.Equal(first.CreatedAt) || !stored.ExpiresAt.Equal(first.ExpiresAt) ||
		!stored.Placement.Digest.Equal(first.Placement.Digest) {
		t.Fatalf("Get returned %#v, want the record Reserve reported %#v", stored, first)
	}
	requireDemand(t, stored.Placement.Total, first.Placement.Total, "the recorded placement total")

	retry := spec
	retry.at = start.Add(30 * time.Second)
	snapshot := rig.snapshot(retry.at)
	candidate := rig.candidate(snapshot, retry, DefaultLeaseFence, scheduling.DefaultReservationTTL)
	if candidate.ID == first.ID {
		t.Fatal("the retry fixture reused the first reservation identifier; it proves nothing")
	}

	replayed, wasReplay, err := rig.repository.Reserve(ctx, snapshot, candidate, retry.at)
	if err != nil {
		t.Fatalf("Reserve of a retried placement: %v", err)
	}
	if !wasReplay {
		t.Fatal("a retried placement was not reported as a replay")
	}
	if replayed.ID != first.ID {
		t.Fatalf("replay returned reservation %q, want the original %q", replayed.ID, first.ID)
	}
	if replayed.Version.String() != first.Version.String() {
		t.Fatalf("replay returned version %q, want the untouched %q", replayed.Version, first.Version)
	}
	if !replayed.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("replay re-dated the reservation to %s, want %s", replayed.CreatedAt, first.CreatedAt)
	}

	// The whole point: one placement, one charge.
	requireDemand(t, rig.reserved(batch, retry.at), charged, "the ledger after a replayed placement")
	if _, err := rig.repository.Get(ctx, candidate.ID); err == nil {
		t.Fatalf("the replayed candidate's identifier %q was recorded", candidate.ID)
	} else {
		requireFault(t, err, faults.CodeNotFound, "reservation_not_found", "Get of a replayed candidate's identifier")
	}

	// Replay is decided before the fence. A redelivery handled by a replica
	// that has since lost leadership still has to be told what happened to the
	// item it is holding, and refusing it as stale would leave the queue
	// retrying forever against a hold that already exists.
	late := rig.candidate(rig.snapshot(retry.at), retry, DefaultLeaseFence-1, scheduling.DefaultReservationTTL)
	stale, wasReplay, err := rig.repository.Reserve(ctx, rig.snapshot(retry.at), late, retry.at)
	if err != nil {
		t.Fatalf("Reserve of a retried placement under an older fence: %v", err)
	}
	if !wasReplay || stale.ID != first.ID {
		t.Fatalf("an older fence changed the replay answer: replayed=%t id=%q", wasReplay, stale.ID)
	}
}

// placementKeyReusedVersusTerminal separates the two faults a caller has to be
// able to tell apart, and pins the order they are decided in.
//
// placement_key_reused means someone else's workload is already using these run
// coordinates; the caller has a bug. reservation_terminal means this exact
// placement already ran to a conclusion; the caller is late. An adapter that
// checked liveness before identity would answer the second question when the
// first one was asked.
func placementKeyReusedVersusTerminal(t *testing.T, factory Factory) {
	t.Helper()
	rig := newRig(t, factory)
	ctx := context.Background()
	rig.seedBatch()

	spec := batchSpec(t, tenantAlpha, start)
	original := rig.reserve(spec, DefaultLeaseFence, scheduling.DefaultReservationTTL)

	// Same run, stage, and attempt; a different workload underneath.
	imposter := spec
	imposter.at = start.Add(time.Minute)
	imposter.workload = newID(t, "workload")
	snapshot := rig.snapshot(imposter.at)
	candidate := rig.candidate(snapshot, imposter, DefaultLeaseFence, scheduling.DefaultReservationTTL)
	_, _, err := rig.repository.Reserve(ctx, snapshot, candidate, imposter.at)
	requireFault(t, err, faults.CodeConflict, "placement_key_reused",
		"Reserve of a different placement under a live key")

	// Retire the original without ever binding it.
	released, wasReplay, err := rig.repository.Release(
		ctx, original.ID, original.Version, DefaultLeaseFence, imposter.at)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if wasReplay || released.State != scheduling.ReservationReleased {
		t.Fatalf("Release returned state %q replayed=%t", released.State, wasReplay)
	}

	// The same placement, now that its hold is terminal.
	late := spec
	late.at = start.Add(2 * time.Minute)
	lateSnapshot := rig.snapshot(late.at)
	lateCandidate := rig.candidate(lateSnapshot, late, DefaultLeaseFence, scheduling.DefaultReservationTTL)
	_, _, err = rig.repository.Reserve(ctx, lateSnapshot, lateCandidate, late.at)
	requireFault(t, err, faults.CodeFailedPrecondition, "reservation_terminal",
		"Reserve of a placement whose reservation is terminal")

	// A different placement under the same key, still terminal. Identity is
	// decided first, so the answer does not change.
	imposter.at = start.Add(3 * time.Minute)
	imposterSnapshot := rig.snapshot(imposter.at)
	imposterCandidate := rig.candidate(imposterSnapshot, imposter, DefaultLeaseFence, scheduling.DefaultReservationTTL)
	_, _, err = rig.repository.Reserve(ctx, imposterSnapshot, imposterCandidate, imposter.at)
	requireFault(t, err, faults.CodeConflict, "placement_key_reused",
		"Reserve of a different placement under a terminal key")

	// One reservation identifier, one reservation. A placement key nothing has
	// claimed still cannot borrow an identifier that is already recorded: both
	// identities are real and neither derives the other, so collapsing them
	// would make a replayed placement unaddressable by the identifier the first
	// attempt returned.
	fresh := batchSpec(t, tenantAlpha, start.Add(4*time.Minute))
	freshSnapshot := rig.snapshot(fresh.at)
	placement, err := freshSnapshot.Place(fresh.placementRequest(), fresh.at)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	duplicate, err := scheduling.NewReservation(
		original.ID, placement, DefaultLeaseFence, scheduling.DefaultReservationTTL)
	if err != nil {
		t.Fatalf("seal a reservation on a recorded identifier: %v", err)
	}
	_, _, err = rig.repository.Reserve(ctx, freshSnapshot, duplicate, fresh.at)
	requireFault(t, err, faults.CodeConflict, "reservation_id_conflict",
		"Reserve reusing a recorded reservation identifier")

	// Every refusal above named a freshly minted reservation identifier, and
	// none of them may have been recorded: a placement key that already has a
	// reservation never mints a second one.
	for _, refused := range []scheduling.Reservation{candidate, lateCandidate, imposterCandidate} {
		_, err = rig.repository.Get(ctx, refused.ID)
		requireFault(t, err, faults.CodeNotFound, "reservation_not_found",
			"Get of the refused candidate "+refused.ID.String())
	}
}

// ledgerRecheckedInsideTheWrite proves the store does not take the caller's
// decision on trust.
//
// The candidate below is sealed against a fleet twice the size of the recorded
// one, and the snapshot handed to Reserve is the real one, so the fingerprint
// comparison passes and the ledger check is the thing that has to refuse it.
// An adapter that admitted whatever a valid Placement asked for would
// over-commit the queue while reporting success -- which is precisely what two
// schedulers sharing a stale view do.
func ledgerRecheckedInsideTheWrite(t *testing.T, factory Factory) {
	t.Helper()
	rig := newRig(t, factory)
	ctx := context.Background()
	batch := domainFor(t, scheduling.WorkloadClassBatchCPU)
	training := domainFor(t, scheduling.WorkloadClassTrainingH100)
	rig.seedBatch()

	oversized := batchSpec(t, tenantAlpha, start)
	oversized.demand = batchQuota()
	oversized.replicas = 2

	inflated := inflatedSnapshot(t, batch, scaleDemand(batchQuota(), 4), start)
	candidate := sealCandidate(t, inflated, oversized, DefaultLeaseFence, scheduling.DefaultReservationTTL)

	actual := rig.snapshot(start)
	_, _, err := rig.repository.Reserve(ctx, actual, candidate, start)
	requireFault(t, err, faults.CodeResourceExhausted, "capacity_exhausted",
		"Reserve of a placement that does not fit the recorded quota")
	if _, err := rig.repository.Get(ctx, candidate.ID); err == nil {
		t.Fatalf("a refused reservation %q was recorded anyway", candidate.ID)
	}
	if reserved := rig.reserved(batch, start); !reserved.IsZero() {
		t.Fatalf("a refused reservation charged %v to the ledger", reserved)
	}

	// The same trick against a domain nobody measured. An absent ledger is not
	// an empty one, so this is not a shortfall -- it is a queue the store has
	// no observation of, and admitting against it is the fail-open.
	gpu := trainingSpec(t, tenantAlpha, start)
	invented := inflatedSnapshot(t, training, trainingQuota(), start)
	unobserved := sealCandidate(t, invented, gpu, DefaultLeaseFence, scheduling.DefaultReservationTTL)
	_, _, err = rig.repository.Reserve(ctx, rig.snapshot(start), unobserved, start)
	requireFault(t, err, faults.CodeNotFound, "capacity_domain_unobserved",
		"Reserve into a capacity domain the store never measured")
}

// fenceMonotonicity is the fencing property stated for the capacity ledger. A
// replaced leader still holding an old token must not be able to move
// capacity, and the store's floor must only ever rise.
func fenceMonotonicity(t *testing.T, factory Factory) {
	t.Helper()
	rig := newRig(t, factory)
	ctx := context.Background()
	rig.seedBatch()

	spec := batchSpec(t, tenantAlpha, start)
	held := rig.reserve(spec, DefaultLeaseFence, scheduling.DefaultReservationTTL)
	at := start.Add(time.Minute)

	_, _, err := rig.repository.Bind(ctx, held.ID, held.Version, boundAssignment(), DefaultLeaseFence-1, at)
	requireFault(t, err, faults.CodeConflict, "lease_fence_stale", "Bind under an older fence")

	// Zero is never authority, and it is rejected before the store's own fence
	// is consulted -- an unfenced writer is not a late writer.
	_, _, err = rig.repository.Bind(ctx, held.ID, held.Version, boundAssignment(), 0, at)
	requireFault(t, err, faults.CodeInvalidArgument, "lease_fence_invalid", "Bind with no fence")

	// Neither refusal moved the floor: the current leader still writes.
	bound, wasReplay, err := rig.repository.Bind(ctx, held.ID, held.Version, boundAssignment(), DefaultLeaseFence, at)
	if err != nil || wasReplay {
		t.Fatalf("Bind under the current fence: %v (replayed=%t)", err, wasReplay)
	}
	if bound.LeaseFence != DefaultLeaseFence {
		t.Fatalf("bound reservation carries fence %d, want %d", bound.LeaseFence, DefaultLeaseFence)
	}

	// A successor's higher fence is accepted and becomes the new floor.
	later := at.Add(time.Minute)
	completed, _, err := rig.repository.Complete(ctx, bound.ID, bound.Version, DefaultLeaseFence+1, later)
	if err != nil {
		t.Fatalf("Complete under a newer fence: %v", err)
	}
	if completed.LeaseFence != DefaultLeaseFence+1 {
		t.Fatalf("completed reservation carries fence %d, want %d", completed.LeaseFence, DefaultLeaseFence+1)
	}

	// ... and the displaced leader is now locked out of every kind of write,
	// including one that names a reservation it has never seen.
	_, _, err = rig.repository.Expire(ctx, bound.ID, bound.Version, DefaultLeaseFence, later)
	requireFault(t, err, faults.CodeConflict, "lease_fence_stale", "Expire under the superseded fence")
	_, _, err = rig.repository.Release(ctx, newID(t, "reservation"), bound.Version, DefaultLeaseFence, later)
	requireFault(t, err, faults.CodeConflict, "lease_fence_stale",
		"Release of an unknown reservation under the superseded fence")

	// Reserve carries the same floor. The fence is checked after the placement
	// key and the reservation identifier, so this uses a placement nothing has
	// claimed.
	fresh := batchSpec(t, tenantAlpha, later)
	snapshot := rig.snapshot(later)
	candidate := rig.candidate(snapshot, fresh, DefaultLeaseFence, scheduling.DefaultReservationTTL)
	_, _, err = rig.repository.Reserve(ctx, snapshot, candidate, later)
	requireFault(t, err, faults.CodeConflict, "lease_fence_stale", "Reserve under the superseded fence")

	// Recording fleet configuration is not a fenced capacity write. It mints an
	// epoch, because it changes what a snapshot says, and it must leave the
	// fence floor exactly where it was -- neither raising it, which would lock
	// out the leader that just wrote, nor lowering it, which would let the
	// displaced one back in.
	before := rig.snapshot(later).Epoch
	rig.putQuota(domainFor(t, scheduling.WorkloadClassBatchCPU), batchQuota())
	if after := rig.snapshot(later).Epoch; after <= before {
		t.Fatalf("recording a quota left the epoch at %d", after)
	}
	_, _, err = rig.repository.Complete(ctx, completed.ID, completed.Version, DefaultLeaseFence, later)
	requireFault(t, err, faults.CodeConflict, "lease_fence_stale",
		"the superseded fence after fleet configuration was recorded")

	surviving := batchSpec(t, tenantAlpha, later)
	survivingSnapshot := rig.snapshot(later)
	survivor := rig.candidate(survivingSnapshot, surviving, DefaultLeaseFence+1, scheduling.DefaultReservationTTL)
	if _, _, err := rig.repository.Reserve(ctx, survivingSnapshot, survivor, later); err != nil {
		t.Fatalf("the current leader was locked out after fleet configuration was recorded: %v", err)
	}
}

// snapshotStaleness is the check that stops two schedulers from admitting
// against the same view of a fleet that has since moved.
//
// The comparison is a digest of the whole snapshot, so it has to be sensitive
// to a ledger that changed and insensitive to a counter that merely advanced.
// Both halves are asserted, because an adapter that compared epochs instead
// would look correct until an unrelated write invalidated every in-flight
// decision at once.
func snapshotStaleness(t *testing.T, factory Factory) {
	t.Helper()
	rig := newRig(t, factory)
	ctx := context.Background()
	rig.seedBatch()

	stale := rig.snapshot(start)
	rig.reserve(batchSpec(t, tenantAlpha, start), DefaultLeaseFence, scheduling.DefaultReservationTTL)

	// The pre-move view no longer describes the store.
	candidate := rig.candidate(stale, batchSpec(t, tenantAlpha, start), DefaultLeaseFence, scheduling.DefaultReservationTTL)
	_, _, err := rig.repository.Reserve(ctx, stale, candidate, start)
	requireFault(t, err, faults.CodeConflict, "fleet_snapshot_stale", "Reserve against a superseded fleet view")
	if _, err := rig.repository.Get(ctx, candidate.ID); err == nil {
		t.Fatalf("a stale decision recorded reservation %q anyway", candidate.ID)
	}

	// A fresh read of the same fleet lands.
	rig.reserve(batchSpec(t, tenantAlpha, start), DefaultLeaseFence, scheduling.DefaultReservationTTL)

	// The fingerprint seals the observation time as well as the ledgers, which
	// is what makes the comparison a decision check rather than a clock race:
	// the caller has to have read the fleet at the instant it is writing at.
	current := rig.snapshot(start)
	later := start.Add(time.Second)
	displaced := rig.candidate(current, batchSpec(t, tenantAlpha, later), DefaultLeaseFence, scheduling.DefaultReservationTTL)
	_, _, err = rig.repository.Reserve(ctx, current, displaced, later)
	requireFault(t, err, faults.CodeConflict, "fleet_snapshot_stale",
		"Reserve against a fleet view observed at another instant")

	// An epoch that advanced without changing anything a decision depends on is
	// not staleness. Re-recording a weight it already had moves the counter and
	// leaves the claims exactly where they were.
	at := start.Add(2 * time.Second)
	before := rig.snapshot(at)
	rig.putWeight(tenantAlpha, 1)
	after := rig.snapshot(at)
	if after.Epoch <= before.Epoch {
		t.Fatalf("recording a weight left the epoch at %d", after.Epoch)
	}
	if !after.Fingerprint().Equal(before.Fingerprint()) {
		t.Fatal("re-recording an unchanged weight changed the fleet fingerprint")
	}
	live := rig.candidate(before, batchSpec(t, tenantAlpha, at), DefaultLeaseFence, scheduling.DefaultReservationTTL)
	if _, _, err := rig.repository.Reserve(ctx, before, live, at); err != nil {
		t.Fatalf("an epoch-only change made a live decision stale: %v", err)
	}
}

// transitionPreconditions covers the reservation lifecycle and, just as
// importantly, the order the preconditions are decided in. The order is
// observable: a request that is wrong in more than one way has to get the same
// answer from either adapter, or a caller switching on the reason branches
// differently depending on which one it is wired to.
func transitionPreconditions(t *testing.T, factory Factory) {
	t.Helper()
	batch := domainFor(t, scheduling.WorkloadClassBatchCPU)

	t.Run("Lifecycle", func(t *testing.T) {
		rig := newRig(t, factory)
		ctx := context.Background()
		rig.seedBatch()
		request := batchSpec(t, tenantAlpha, start)
		held := rig.reserve(request, DefaultLeaseFence, scheduling.DefaultReservationTTL)
		at := start.Add(time.Minute)

		// Completion is not a shortcut through binding: capacity the cluster
		// never accepted cannot have finished using itself.
		_, _, err := rig.repository.Complete(ctx, held.ID, held.Version, DefaultLeaseFence, at)
		requireFault(t, err, faults.CodeConflict, "reservation_complete_invalid", "Complete of a held reservation")

		// Expiring early would free capacity the holder is still entitled to.
		_, _, err = rig.repository.Expire(ctx, held.ID, held.Version, DefaultLeaseFence, at)
		requireFault(t, err, faults.CodeFailedPrecondition, "reservation_not_expired", "Expire before the deadline")

		// A recorded assignment that contradicts the sealed constraint means
		// the cluster placed the workload somewhere the decision did not
		// authorize; accepting it would make the reservation lie.
		_, _, err = rig.repository.Bind(ctx, held.ID, held.Version, mismatchedAssignment(), DefaultLeaseFence, at)
		requireFault(t, err, faults.CodeConflict, "reservation_assignment_mismatch",
			"Bind with an assignment the placement did not authorize")

		bound, wasReplay, err := rig.repository.Bind(ctx, held.ID, held.Version, boundAssignment(), DefaultLeaseFence, at)
		if err != nil || wasReplay {
			t.Fatalf("Bind: %v (replayed=%t)", err, wasReplay)
		}
		if bound.State != scheduling.ReservationBound || bound.Sequence != 1 {
			t.Fatalf("bound reservation is %q at sequence %d, want bound at 1", bound.State, bound.Sequence)
		}
		if !bound.BoundAt.Equal(at) {
			t.Fatalf("bound at %s, want %s", bound.BoundAt, at)
		}
		// The assignment survives the round trip intact. It feeds the version
		// digest, so an adapter that dropped the recorded domains would fail to
		// revalidate the record it just wrote.
		if len(bound.Assignment.Domains) != 1 || bound.Assignment.Domains[0] != boundAssignment().Domains[0] {
			t.Fatalf("recorded topology assignment = %#v, want %#v",
				bound.Assignment.Domains, boundAssignment().Domains)
		}

		// Binding is the point after which a clock can no longer release the
		// hold, not the point where it stops being charged. A ledger that
		// dropped bound reservations would report capacity as free while the
		// cluster was running work on it.
		requireDemand(t, rig.reserved(batch, at), request.total(t), "the ledger after a bind")
		if active := rig.held(batch, at); len(active) != 1 || active[0].ID != bound.ID {
			t.Fatalf("a bound reservation is not reported as occupying capacity: %#v", active)
		}

		// The redelivered bind carries the version from before the write it is
		// replaying, and must still be answered with the record that landed.
		again, wasReplay, err := rig.repository.Bind(
			ctx, held.ID, held.Version, boundAssignment(), DefaultLeaseFence, at.Add(time.Second))
		if err != nil {
			t.Fatalf("redelivered Bind: %v", err)
		}
		if !wasReplay {
			t.Fatal("a redelivered Bind was not reported as a replay")
		}
		if again.Version.String() != bound.Version.String() || again.Sequence != bound.Sequence {
			t.Fatalf("the replay moved the record to %q at sequence %d", again.Version, again.Sequence)
		}

		// Release returns capacity that was never bound; it is not a way to
		// abandon a running workload.
		_, _, err = rig.repository.Release(ctx, bound.ID, bound.Version, DefaultLeaseFence, at)
		requireFault(t, err, faults.CodeConflict, "reservation_release_invalid", "Release of a bound reservation")

		done, wasReplay, err := rig.repository.Complete(ctx, bound.ID, bound.Version, DefaultLeaseFence, at)
		if err != nil || wasReplay {
			t.Fatalf("Complete: %v (replayed=%t)", err, wasReplay)
		}
		if done.State != scheduling.ReservationCompleted || done.Sequence != 2 {
			t.Fatalf("completed reservation is %q at sequence %d, want completed at 2", done.State, done.Sequence)
		}
		if !done.FinalizedAt.Equal(at) {
			t.Fatalf("finalized at %s, want %s", done.FinalizedAt, at)
		}

		if recorded := rig.get(done.ID); recorded.Version.String() != done.Version.String() {
			t.Fatalf("the completed record came back as %q, want %q", recorded.Version, done.Version)
		}

		retired := at.Add(time.Minute)
		_, _, err = rig.repository.Release(ctx, done.ID, done.Version, DefaultLeaseFence, retired)
		requireFault(t, err, faults.CodeFailedPrecondition, "reservation_terminal", "Release of a completed reservation")
		if reserved := rig.reserved(batch, retired); !reserved.IsZero() {
			t.Fatalf("a completed reservation still charges %v to the ledger", reserved)
		}
	})

	t.Run("CheckOrder", func(t *testing.T) {
		rig := newRig(t, factory)
		ctx := context.Background()
		rig.seedBatch()
		held := rig.reserve(batchSpec(t, tenantAlpha, start), DefaultLeaseFence, scheduling.DefaultReservationTTL)
		at := start.Add(time.Minute)

		released, _, err := rig.repository.Release(ctx, held.ID, held.Version, DefaultLeaseFence, at)
		if err != nil {
			t.Fatalf("Release: %v", err)
		}

		// Replay is decided before terminality. The worker crashed between the
		// commit and the ack, so the caller is holding the pre-release version
		// and is entitled to the record that landed.
		again, wasReplay, err := rig.repository.Release(ctx, held.ID, held.Version, DefaultLeaseFence, at.Add(time.Second))
		if err != nil || !wasReplay {
			t.Fatalf("redelivered Release: %v (replayed=%t)", err, wasReplay)
		}
		if again.Version.String() != released.Version.String() {
			t.Fatalf("the replay moved the record to %q, want %q", again.Version, released.Version)
		}

		// Terminality is decided before the version precondition. The caller's
		// version really is stale here, and the useful answer is still that the
		// reservation will never move again.
		_, _, err = rig.repository.Complete(ctx, held.ID, held.Version, DefaultLeaseFence, at.Add(time.Second))
		requireFault(t, err, faults.CodeFailedPrecondition, "reservation_terminal",
			"a different transition on a terminal reservation held at a stale version")

		// A live reservation held at a stale version is a stale version.
		other := rig.reserve(batchSpec(t, tenantAlpha, start), DefaultLeaseFence, scheduling.DefaultReservationTTL)
		if _, _, err := rig.repository.Bind(
			ctx, other.ID, other.Version, boundAssignment(), DefaultLeaseFence, at); err != nil {
			t.Fatalf("Bind: %v", err)
		}
		_, _, err = rig.repository.Complete(ctx, other.ID, other.Version, DefaultLeaseFence, at)
		requireFault(t, err, faults.CodeConflict, "reservation_version_stale",
			"a transition on a live reservation held at a stale version")
	})

	t.Run("Addressing", func(t *testing.T) {
		rig := newRig(t, factory)
		ctx := context.Background()
		rig.seedBatch()
		held := rig.reserve(batchSpec(t, tenantAlpha, start), DefaultLeaseFence, scheduling.DefaultReservationTTL)
		at := start.Add(time.Minute)

		_, _, err := rig.repository.Bind(
			ctx, newID(t, "reservation"), held.Version, boundAssignment(), DefaultLeaseFence, at)
		requireFault(t, err, faults.CodeNotFound, "reservation_not_found", "Bind of an unrecorded reservation")

		_, _, err = rig.repository.Bind(
			ctx, identifiers.ID{}, held.Version, boundAssignment(), DefaultLeaseFence, at)
		requireFault(t, err, faults.CodeInvalidArgument, "reservation_id_invalid", "Bind with no reservation identifier")

		// A canonical identifier of the wrong kind is still not a reservation.
		_, _, err = rig.repository.Bind(
			ctx, newID(t, "workload"), held.Version, boundAssignment(), DefaultLeaseFence, at)
		requireFault(t, err, faults.CodeInvalidArgument, "reservation_id_invalid", "Bind with a workload identifier")

		_, _, err = rig.repository.Bind(
			ctx, held.ID, resourceversion.Version{}, boundAssignment(), DefaultLeaseFence, at)
		requireFault(t, err, faults.CodeInvalidArgument, "expected_version_invalid", "Bind with no expected version")
	})
}

// expiryIsAWrite is the difference between capacity that is busy and capacity
// that is merely unaccounted for.
//
// A lapsed hold has to stop counting against the ledger before anything reads
// it, and the only way to make the sweep and the read agree is to perform the
// sweep as part of the read. Get is the exception, and deliberately so: it
// names one row and consults no ledger, so it has nothing to expire and reports
// what is recorded.
func expiryIsAWrite(t *testing.T, factory Factory) {
	t.Helper()
	batch := domainFor(t, scheduling.WorkloadClassBatchCPU)
	// One second past a one-second hold.
	lapsed := start.Add(2 * time.Second)

	t.Run("HeldReSealsALapsedHold", func(t *testing.T) {
		rig := newRig(t, factory)
		rig.seedBatch()
		held := rig.reserve(batchSpec(t, tenantAlpha, start), DefaultLeaseFence, scheduling.MinimumReservationTTL)

		before := rig.get(held.ID)
		if before.State != scheduling.ReservationHeld {
			t.Fatalf("Get swept a lapsed hold to %q; Get is not a ledger read", before.State)
		}
		if before.Version.String() != held.Version.String() {
			t.Fatalf("Get moved the record to %q", before.Version)
		}

		if active := rig.held(batch, lapsed); len(active) != 0 {
			t.Fatalf("a lapsed hold is still reported as occupying capacity: %#v", active)
		}

		swept := rig.get(held.ID)
		if swept.State != scheduling.ReservationExpired || swept.Sequence != 1 {
			t.Fatalf("after Held the record is %q at sequence %d, want expired at 1", swept.State, swept.Sequence)
		}
		if !swept.FinalizedAt.Equal(lapsed) {
			t.Fatalf("expired at %s, want the reading transaction's time %s", swept.FinalizedAt, lapsed)
		}
		// Expiry is authored by the deadline, not by a leader, so it reuses the
		// reservation's own fence. Requiring a live one would make an
		// unattended control plane unable to reclaim anything.
		if swept.LeaseFence != held.LeaseFence {
			t.Fatalf("the sweep restamped the fence to %d, want the reservation's own %d",
				swept.LeaseFence, held.LeaseFence)
		}
	})

	t.Run("SnapshotReSealsALapsedHold", func(t *testing.T) {
		rig := newRig(t, factory)
		rig.seedBatch()
		held := rig.reserve(batchSpec(t, tenantAlpha, start), DefaultLeaseFence, scheduling.MinimumReservationTTL)

		if reserved := rig.reserved(batch, lapsed); !reserved.IsZero() {
			t.Fatalf("a lapsed hold still charges %v to the ledger", reserved)
		}
		if swept := rig.get(held.ID); swept.State != scheduling.ReservationExpired {
			t.Fatalf("after Snapshot the record is %q, want expired", swept.State)
		}
	})

	t.Run("TheSweepMintsNoEpoch", func(t *testing.T) {
		rig := newRig(t, factory)
		rig.seedBatch()
		rig.reserve(batchSpec(t, tenantAlpha, start), DefaultLeaseFence, scheduling.MinimumReservationTTL)

		// The sweep is not a decision. Minting an epoch for it would make every
		// snapshot a caller is holding stale the moment someone else's read
		// drained a backlog of lapsed holds -- during recovery, which is
		// exactly when the sweep is supposed to be helping.
		live := rig.snapshot(start.Add(500 * time.Millisecond)).Epoch
		swept := rig.snapshot(lapsed).Epoch
		if swept != live {
			t.Fatalf("the expiry sweep moved the epoch from %d to %d", live, swept)
		}
	})

	t.Run("ExpiryIsReplayedNotRefused", func(t *testing.T) {
		rig := newRig(t, factory)
		ctx := context.Background()
		rig.seedBatch()
		held := rig.reserve(batchSpec(t, tenantAlpha, start), DefaultLeaseFence, scheduling.MinimumReservationTTL)
		rig.snapshot(lapsed)

		// The reconcile path asks for the expiry the sweep already performed.
		// That is a replay of a write that landed, not a refusal.
		expired, wasReplay, err := rig.repository.Expire(ctx, held.ID, held.Version, DefaultLeaseFence, lapsed)
		if err != nil {
			t.Fatalf("Expire of an already-swept hold: %v", err)
		}
		if !wasReplay || expired.State != scheduling.ReservationExpired {
			t.Fatalf("Expire of an already-swept hold = %q (replayed=%t)", expired.State, wasReplay)
		}

		// Every other transition is refused, because the hold really is gone.
		_, _, err = rig.repository.Bind(ctx, held.ID, held.Version, boundAssignment(), DefaultLeaseFence, lapsed)
		requireFault(t, err, faults.CodeFailedPrecondition, "reservation_terminal", "Bind of a swept hold")
	})

	t.Run("ARetriedPlacementCannotResurrectALapsedHold", func(t *testing.T) {
		rig := newRig(t, factory)
		ctx := context.Background()
		rig.seedBatch()
		spec := batchSpec(t, tenantAlpha, start)
		rig.reserve(spec, DefaultLeaseFence, scheduling.MinimumReservationTTL)

		// Deliberately a view of the fleet from before the deadline, never
		// refreshed. If Reserve did not sweep before it looked the placement
		// key up it would find a live hold and report a replay, so this
		// distinguishes an adapter that expires inside the write from one that
		// expires somewhere else.
		before := rig.snapshot(start)
		retry := spec
		retry.at = lapsed
		candidate := rig.candidate(before, retry, DefaultLeaseFence, scheduling.DefaultReservationTTL)
		_, _, err := rig.repository.Reserve(ctx, before, candidate, lapsed)
		requireFault(t, err, faults.CodeFailedPrecondition, "reservation_terminal",
			"Reserve of a placement whose hold has lapsed")
		if reserved := rig.reserved(batch, lapsed); !reserved.IsZero() {
			t.Fatalf("the refused retry left %v charged to the ledger", reserved)
		}
	})
}

// wholePlanPreemption covers the all-or-nothing eviction contract. A plan is
// acknowledged only when EVERY victim is already this candidate's; a plan that
// landed in part is re-applied rather than acknowledged, because acknowledging
// it would report evictions that never happened.
func wholePlanPreemption(t *testing.T, factory Factory) {
	t.Helper()
	batch := domainFor(t, scheduling.WorkloadClassBatchCPU)

	t.Run("PlanAppliesAndReplays", func(t *testing.T) {
		rig := newRig(t, factory)
		ctx := context.Background()
		rig.seedBatch()
		at := start.Add(time.Minute)
		firstVictim := rig.reserve(batchSpec(t, tenantZeta, start), DefaultLeaseFence, scheduling.DefaultReservationTTL)
		secondVictim := rig.reserve(batchSpec(t, tenantZeta, start), DefaultLeaseFence, scheduling.DefaultReservationTTL)

		// One victim bound and one merely held: preemption seals a different
		// sequence for each, and an adapter that assumed one shape would pass a
		// homogeneous plan and corrupt the other.
		bound, _, err := rig.repository.Bind(
			ctx, secondVictim.ID, secondVictim.Version, boundAssignment(), DefaultLeaseFence, at)
		if err != nil {
			t.Fatalf("Bind: %v", err)
		}
		candidate := newID(t, "reservation")

		// The realistic route first: the adapter's own Held output, ranked
		// against the adapter's own fair-share view, has to yield a usable
		// plan. A held set that under-reported would silently choose fewer
		// victims than the shortfall needs.
		if selected := selectPlan(t, rig, batch, candidate, at); len(selected.Victims) != 2 {
			t.Fatalf("SelectVictims chose %d victims from the adapter's held set, want 2", len(selected.Victims))
		}

		plan := victimPlan(t, candidate, batch, firstVictim, bound)
		_, _, err = rig.repository.Preempt(ctx, plan, 0, at)
		requireFault(t, err, faults.CodeInvalidArgument, "lease_fence_invalid", "Preempt with no fence")
		_, _, err = rig.repository.Preempt(ctx, plan, DefaultLeaseFence-1, at)
		requireFault(t, err, faults.CodeConflict, "lease_fence_stale", "Preempt under an older fence")
		_, _, err = rig.repository.Preempt(ctx, plan, DefaultLeaseFence, time.Time{})
		requireFault(t, err, faults.CodeInvalidArgument, "snapshot_time_invalid", "Preempt with no transaction time")

		evicted, wasReplay, err := rig.repository.Preempt(ctx, plan, DefaultLeaseFence, at)
		if err != nil || wasReplay {
			t.Fatalf("Preempt: %v (replayed=%t)", err, wasReplay)
		}
		if len(evicted) != 2 {
			t.Fatalf("Preempt evicted %d reservations, want the whole plan's 2", len(evicted))
		}
		wantSequence := map[identifiers.ID]uint32{firstVictim.ID: 1, bound.ID: 2}
		versions := make(map[identifiers.ID]string, len(evicted))
		for _, reservation := range evicted {
			if reservation.State != scheduling.ReservationPreempted {
				t.Fatalf("victim %q is %q, want preempted", reservation.ID, reservation.State)
			}
			if reservation.Preemptor != candidate {
				t.Fatalf("victim %q names preemptor %q, want %q", reservation.ID, reservation.Preemptor, candidate)
			}
			if want, ok := wantSequence[reservation.ID]; !ok || reservation.Sequence != want {
				t.Fatalf("victim %q sealed at sequence %d, want %d", reservation.ID, reservation.Sequence, want)
			}
			versions[reservation.ID] = reservation.Version.String()

			// Reading the record back re-derives the digest that seals it, so
			// this is the whole transition -- state, sequence, finalization
			// time, and the preemptor identifier -- surviving whatever encoding
			// the adapter stores it in. A dropped preemptor makes the record
			// fail to revalidate rather than come back subtly wrong.
			recorded := rig.get(reservation.ID)
			if recorded.Version.String() != reservation.Version.String() || recorded.Preemptor != candidate {
				t.Fatalf("victim %q was recorded as %#v, want %#v", reservation.ID, recorded, reservation)
			}
		}
		if reserved := rig.reserved(batch, at); !reserved.IsZero() {
			t.Fatalf("preempted victims still charge %v to the ledger", reserved)
		}

		// The redelivered plan is an acknowledgement, not a second eviction.
		replayed, wasReplay, err := rig.repository.Preempt(ctx, plan, DefaultLeaseFence, at.Add(time.Minute))
		if err != nil {
			t.Fatalf("redelivered Preempt: %v", err)
		}
		if !wasReplay {
			t.Fatal("a redelivered preemption plan was not reported as a replay")
		}
		if len(replayed) != len(evicted) {
			t.Fatalf("the replay returned %d reservations, want %d", len(replayed), len(evicted))
		}
		for _, reservation := range replayed {
			if versions[reservation.ID] != reservation.Version.String() {
				t.Fatalf("the replay moved victim %q to %q, want %q",
					reservation.ID, reservation.Version, versions[reservation.ID])
			}
		}
	})

	t.Run("APartlyAppliedPlanIsNotAReplay", func(t *testing.T) {
		rig := newRig(t, factory)
		ctx := context.Background()
		rig.seedBatch()
		at := start.Add(time.Minute)
		first := rig.reserve(batchSpec(t, tenantZeta, start), DefaultLeaseFence, scheduling.DefaultReservationTTL)
		second := rig.reserve(batchSpec(t, tenantZeta, start), DefaultLeaseFence, scheduling.DefaultReservationTTL)
		candidate := newID(t, "reservation")

		whole := victimPlan(t, candidate, batch, first, second)
		part := victimPlan(t, candidate, batch, first)
		if _, _, err := rig.repository.Preempt(ctx, part, DefaultLeaseFence, at); err != nil {
			t.Fatalf("Preempt of the one-victim plan: %v", err)
		}

		// Every victim has to match for a plan to be an acknowledgement, so the
		// two-victim plan is re-applied -- and cannot be, because the victim
		// that already went no longer holds what the plan was built from.
		_, _, err := rig.repository.Preempt(ctx, whole, DefaultLeaseFence, at)
		requireFault(t, err, faults.CodeConflict, "preemption_victim_changed",
			"Preempt of a plan only one of whose victims was applied")
		if survivor := rig.get(second.ID); survivor.State != scheduling.ReservationHeld {
			t.Fatalf("the untouched victim is %q, want held", survivor.State)
		}
	})

	t.Run("MissingVictim", func(t *testing.T) {
		rig := newRig(t, factory)
		rig.seedBatch()
		ghost := rig.candidate(rig.snapshot(start), batchSpec(t, tenantZeta, start),
			DefaultLeaseFence, scheduling.DefaultReservationTTL)
		plan := victimPlan(t, newID(t, "reservation"), batch, ghost)

		_, _, err := rig.repository.Preempt(context.Background(), plan, DefaultLeaseFence, start.Add(time.Minute))
		requireFault(t, err, faults.CodeNotFound, "preemption_victim_not_found",
			"Preempt of a plan naming an unrecorded victim")
	})
}

// argumentValidation covers the checks that run before either adapter reaches
// its storage, where the reference adapter's map and the store's transaction
// have the most freedom to disagree about what counts as a bad request.
func argumentValidation(t *testing.T, factory Factory) {
	t.Helper()
	rig := newRig(t, factory)
	ctx := context.Background()
	batch := domainFor(t, scheduling.WorkloadClassBatchCPU)
	rig.seedBatch()

	_, err := rig.repository.Snapshot(ctx, time.Time{})
	requireFault(t, err, faults.CodeInvalidArgument, "snapshot_time_invalid", "Snapshot with no transaction time")
	_, err = rig.repository.Held(ctx, batch, time.Time{})
	requireFault(t, err, faults.CodeInvalidArgument, "snapshot_time_invalid", "Held with no transaction time")

	// An unresolved capacity domain names no queue, and there is no fourth
	// admissible triple for it to fall back to.
	_, err = rig.repository.Held(ctx, scheduling.CapacityDomain{}, start)
	requireFault(t, err, faults.CodeInvalidArgument, "workload_class_invalid",
		"Held for an unresolved capacity domain")

	spec := batchSpec(t, tenantAlpha, start)
	snapshot := rig.snapshot(start)
	candidate := rig.candidate(snapshot, spec, DefaultLeaseFence, scheduling.DefaultReservationTTL)
	_, _, err = rig.repository.Reserve(ctx, snapshot, candidate, time.Time{})
	requireFault(t, err, faults.CodeInvalidArgument, "snapshot_time_invalid", "Reserve with no transaction time")

	// The decision time is part of what the placement digest seals, so a
	// candidate sealed at another instant is not this transaction's candidate.
	_, _, err = rig.repository.Reserve(ctx, snapshot, candidate, start.Add(time.Second))
	requireFault(t, err, faults.CodeInvalidArgument, "reservation_created_at_invalid",
		"Reserve of a candidate decided at another instant")

	held := rig.reserve(spec, DefaultLeaseFence, scheduling.DefaultReservationTTL)
	at := start.Add(time.Minute)
	bound, _, err := rig.repository.Bind(ctx, held.ID, held.Version, boundAssignment(), DefaultLeaseFence, at)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	// A reservation that has already moved is not a candidate for a new hold.
	_, _, err = rig.repository.Reserve(ctx, rig.snapshot(at), bound, at)
	requireFault(t, err, faults.CodeInvalidArgument, "reservation_initial_state_invalid",
		"Reserve of an already-bound reservation")

	// A cancelled context ends every call, and this is the one assertion in the
	// suite that checks the code without the reason. The reference adapter
	// returns context.Canceled unwrapped while a durable store wraps it with
	// the operation it abandoned; neither has a domain decision to name, and
	// there is nothing for a caller to switch on. The code is the contract.
	done, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := rig.repository.Snapshot(done, at); !faults.IsCode(err, faults.CodeCanceled) {
		t.Fatalf("Snapshot on a cancelled context = %v, want a cancelled fault", err)
	}
	if _, err := rig.repository.Get(done, bound.ID); !faults.IsCode(err, faults.CodeCanceled) {
		t.Fatalf("Get on a cancelled context = %v, want a cancelled fault", err)
	}
	if _, _, err := rig.repository.Complete(
		done, bound.ID, bound.Version, DefaultLeaseFence, at); !faults.IsCode(err, faults.CodeCanceled) {
		t.Fatalf("Complete on a cancelled context = %v, want a cancelled fault", err)
	}
	if current := rig.get(bound.ID); current.State != scheduling.ReservationBound {
		t.Fatalf("a cancelled Complete moved the reservation to %q", current.State)
	}
}

// harness holds the repository under test and the fleet-configuration surface
// beside it, so a case reads as the sequence of decisions it is asserting on
// rather than as error plumbing.
type harness struct {
	t          *testing.T
	repository scheduling.Repository
	fleet      Fleet
}

func newRig(t *testing.T, factory Factory) *harness {
	t.Helper()
	repository := factory(t)
	// One check, not two. A factory that returned a nil interface fails this
	// assertion as well, and it fails here with a message about the adapter
	// rather than as a nil dereference inside whichever case ran first.
	fleet, ok := repository.(Fleet)
	if !ok {
		t.Fatalf("the repository under test (%T) does not expose PutQuota and PutWeight; "+
			"schedulingtest cannot configure a fleet through Repository alone", repository)
	}
	return &harness{t: t, repository: repository, fleet: fleet}
}

// seedBatch records the fleet every capacity case needs: one active capacity
// domain and two weighted tenants, the tenants in the reverse of their sorted
// order. Every ClusterQueue ships with a zero nominal quota and stopPolicy
// Hold, so without this the domain is inactive and nothing is admissible.
func (rig *harness) seedBatch() {
	rig.t.Helper()
	rig.putQuota(domainFor(rig.t, scheduling.WorkloadClassBatchCPU), batchQuota())
	rig.putWeight(tenantZeta, 1)
	rig.putWeight(tenantAlpha, 1)
}

func (rig *harness) putQuota(domain scheduling.CapacityDomain, nominal scheduling.Demand) {
	rig.t.Helper()
	if err := rig.fleet.PutQuota(context.Background(), domain, nominal); err != nil {
		rig.t.Fatalf("PutQuota %s: %v", domain.WorkloadClass(), err)
	}
}

func (rig *harness) putWeight(tenant string, weight uint32) {
	rig.t.Helper()
	if err := rig.fleet.PutWeight(context.Background(), tenant, weight); err != nil {
		rig.t.Fatalf("PutWeight %s: %v", tenant, err)
	}
}

func (rig *harness) snapshot(at time.Time) scheduling.FleetSnapshot {
	rig.t.Helper()
	snapshot, err := rig.repository.Snapshot(context.Background(), at)
	if err != nil {
		rig.t.Fatalf("Snapshot at %s: %v", at, err)
	}
	return snapshot
}

func (rig *harness) held(domain scheduling.CapacityDomain, at time.Time) []scheduling.Reservation {
	rig.t.Helper()
	active, err := rig.repository.Held(context.Background(), domain, at)
	if err != nil {
		rig.t.Fatalf("Held %s: %v", domain.WorkloadClass(), err)
	}
	return active
}

func (rig *harness) get(id identifiers.ID) scheduling.Reservation {
	rig.t.Helper()
	reservation, err := rig.repository.Get(context.Background(), id)
	if err != nil {
		rig.t.Fatalf("Get %s: %v", id, err)
	}
	return reservation
}

// reserved is the capacity one domain currently charges. It goes through
// Snapshot rather than through a summed Held, because the ledger a scheduler
// decides against is the snapshot's, and the two are allowed to be computed
// differently.
func (rig *harness) reserved(domain scheduling.CapacityDomain, at time.Time) scheduling.Demand {
	rig.t.Helper()
	allocatable, err := rig.snapshot(at).Allocatable(domain)
	if err != nil {
		rig.t.Fatalf("allocatable for %s: %v", domain.WorkloadClass(), err)
	}
	return allocatable.Reserved
}

func (rig *harness) candidate(
	snapshot scheduling.FleetSnapshot, request spec, fence uint64, ttl time.Duration,
) scheduling.Reservation {
	rig.t.Helper()
	return sealCandidate(rig.t, snapshot, request, fence, ttl)
}

// reserve runs the whole Place-then-Reserve round trip the way Service.Handle
// does: read the fleet, decide against that value, and let the store re-check
// the decision inside the write.
func (rig *harness) reserve(request spec, fence uint64, ttl time.Duration) scheduling.Reservation {
	rig.t.Helper()
	snapshot := rig.snapshot(request.at)
	candidate := rig.candidate(snapshot, request, fence, ttl)
	reservation, replayed, err := rig.repository.Reserve(context.Background(), snapshot, candidate, request.at)
	if err != nil {
		rig.t.Fatalf("Reserve: %v", err)
	}
	if replayed {
		rig.t.Fatal("a first reservation reported a replay")
	}
	return reservation
}

// spec is one placement request in the shape the suite needs to vary it: the
// run coordinates that form the placement key, the workload underneath them,
// and the demand.
type spec struct {
	at          time.Time
	tenant      string
	workload    identifiers.ID
	run         string
	stage       string
	attempt     uint32
	demand      scheduling.Demand
	replicas    uint32
	priority    scheduling.PriorityClass
	stageKind   orchestration.StageKind
	pool        scheduling.PoolKind
	accelerator scheduling.Accelerator
}

// batchSpec is a CPU placement bound for the batch-cpu queue. Every identifier
// is freshly minted, so two specs never collide and an adapter that keyed its
// replay on something coarser than the run coordinates fails the replay case
// rather than quietly passing it.
func batchSpec(tb testing.TB, tenant string, at time.Time) spec {
	tb.Helper()
	return spec{
		at:          at,
		tenant:      tenant,
		workload:    newID(tb, "workload"),
		run:         newID(tb, "run").String(),
		stage:       newID(tb, "stage").String(),
		attempt:     1,
		demand:      cpuDemand(2_000, 4*gibibyte, 8*gibibyte, 1),
		replicas:    2,
		priority:    scheduling.PriorityBatch,
		stageKind:   orchestration.StagePreprocess,
		pool:        scheduling.PoolFeaturizationCPU,
		accelerator: scheduling.AcceleratorNone,
	}
}

// trainingSpec is an accelerated placement bound for the training-h100 queue.
// It exists so the snapshot cases cover two capacity domains: the batch-cpu
// queue does not list nvidia.com/gpu among its covered resources at all, so a
// GPU demand is the only way to reach a second ledger.
func trainingSpec(tb testing.TB, tenant string, at time.Time) spec {
	tb.Helper()
	request := batchSpec(tb, tenant, at)
	request.demand = gpuDemand(4_000, 32*gibibyte, 64*gibibyte, 2, 1)
	request.stageKind = orchestration.StageTraining
	request.pool = scheduling.PoolGPUTraining
	request.accelerator = scheduling.AcceleratorH100
	return request
}

func (request spec) placementRequest() scheduling.PlacementRequest {
	return scheduling.PlacementRequest{
		Admission: scheduling.AdmissionRequest{
			WorkloadID:  request.workload,
			Tenant:      request.tenant,
			Workspace:   "conformance",
			StageKind:   request.stageKind,
			Pool:        request.pool,
			Accelerator: request.accelerator,
			Priority:    request.priority,
			Demand:      request.demand.Clone(),
			Replicas:    request.replicas,
		},
		RunID:   request.run,
		StageID: request.stage,
		Attempt: request.attempt,
	}
}

func (request spec) total(tb testing.TB) scheduling.Demand {
	tb.Helper()
	total, err := request.demand.Scale(uint64(request.replicas))
	if err != nil {
		tb.Fatalf("scale the fixture demand: %v", err)
	}
	return total
}

// sealCandidate decides one placement against a snapshot and seals a
// generation-one hold for it. A hand-built Reservation is not a valid one --
// Validate re-derives the digest that seals it -- so every fixture goes through
// the domain's own constructors.
func sealCandidate(
	tb testing.TB, snapshot scheduling.FleetSnapshot, request spec, fence uint64, ttl time.Duration,
) scheduling.Reservation {
	tb.Helper()
	placement, err := snapshot.Place(request.placementRequest(), request.at)
	if err != nil {
		tb.Fatalf("place for %s at %s: %v", request.tenant, request.at, err)
	}
	candidate, err := scheduling.NewReservation(newID(tb, "reservation"), placement, fence, ttl)
	if err != nil {
		tb.Fatalf("seal a reservation: %v", err)
	}
	return candidate
}

// inflatedSnapshot is a fleet value no adapter produced. It exists so a case
// can seal a valid placement against capacity the store does not have, which is
// the only way to reach the ledger re-check inside Reserve: a placement decided
// against the store's real snapshot always fits by construction.
func inflatedSnapshot(
	tb testing.TB, domain scheduling.CapacityDomain, nominal scheduling.Demand, at time.Time,
) scheduling.FleetSnapshot {
	tb.Helper()
	snapshot := scheduling.FleetSnapshot{
		Epoch:      1,
		ObservedAt: at,
		Allocatables: []scheduling.Allocatable{
			{Domain: domain, Nominal: nominal.Clone(), Reserved: make(scheduling.Demand)},
		},
		TopologyDigest: scheduling.TopologyFingerprint(),
	}
	if err := snapshot.Validate(); err != nil {
		tb.Fatalf("the inflated fixture snapshot is not valid: %v", err)
	}
	return snapshot
}

// selectPlan runs the domain's own victim selection over whatever the adapter
// reports as held. Nothing else in the suite connects those two, and a held set
// that under-reported would produce a plan that covers less than the shortfall
// rather than an error anyone would notice.
func selectPlan(
	tb testing.TB, rig *harness, domain scheduling.CapacityDomain,
	candidate identifiers.ID, at time.Time,
) scheduling.PreemptionPlan {
	tb.Helper()
	active := rig.held(domain, at)
	share, err := rig.snapshot(at).FairShare(domain)
	if err != nil {
		tb.Fatalf("fair-share view for %s: %v", domain.WorkloadClass(), err)
	}
	shortfall := make(scheduling.Demand)
	for _, reservation := range active {
		shortfall = sumDemand(shortfall, reservation.HeldDemand())
	}
	plan, err := scheduling.SelectVictims(scheduling.PreemptionRequest{
		Candidate: candidate,
		Domain:    domain,
		Tenant:    tenantAlpha,
		Priority:  scheduling.PriorityPlatformCritical,
		Shortfall: shortfall,
	}, active, share, at)
	if err != nil {
		tb.Fatalf("select victims over the adapter's held set: %v", err)
	}
	return plan
}

// victimPlan builds an eviction set by hand so a case controls exactly which
// reservations it names. SelectVictims picks by rank, which is the right thing
// in production and the wrong thing for a case that needs one specific victim
// evicted before another.
func victimPlan(
	tb testing.TB, candidate identifiers.ID, domain scheduling.CapacityDomain,
	victims ...scheduling.Reservation,
) scheduling.PreemptionPlan {
	tb.Helper()
	entries := make([]scheduling.Victim, 0, len(victims))
	reclaimed := make(scheduling.Demand)
	for _, victim := range victims {
		held := victim.HeldDemand()
		entries = append(entries, scheduling.Victim{
			ReservationID: victim.ID,
			Tenant:        victim.Placement.Tenant,
			Priority:      victim.Placement.Priority,
			Reclaimed:     held,
			Action:        scheduling.ActionEvictAndRequeue,
		})
		reclaimed = sumDemand(reclaimed, held)
	}
	plan := scheduling.PreemptionPlan{
		Candidate: candidate,
		Domain:    domain,
		Shortfall: reclaimed.Clone(),
		Reclaimed: reclaimed,
		Victims:   entries,
	}
	if err := plan.Validate(); err != nil {
		tb.Fatalf("the fixture preemption plan is not valid: %v", err)
	}
	return plan
}

// domainFor resolves one capacity domain out of the closed set. It goes through
// Domains() rather than DomainFor so a case cannot name a triple the domain
// package has stopped admitting.
func domainFor(tb testing.TB, class scheduling.WorkloadClass) scheduling.CapacityDomain {
	tb.Helper()
	for _, domain := range scheduling.Domains() {
		if domain.WorkloadClass() == class {
			return domain
		}
	}
	tb.Fatalf("scheduling.Domains() does not contain the %q capacity domain", class)
	return scheduling.CapacityDomain{}
}

func newID(tb testing.TB, kind string) identifiers.ID {
	tb.Helper()
	id, err := identifiers.NewID(identifiers.MustParseKind(kind))
	if err != nil {
		tb.Fatalf("mint a %s identifier: %v", kind, err)
	}
	return id
}

func cpuDemand(cpu, memory, storage, pods uint64) scheduling.Demand {
	return scheduling.Demand{
		scheduling.ResourceCPU:              cpu,
		scheduling.ResourceMemory:           memory,
		scheduling.ResourceEphemeralStorage: storage,
		scheduling.ResourcePods:             pods,
	}
}

func gpuDemand(cpu, memory, storage, gpu, pods uint64) scheduling.Demand {
	demand := cpuDemand(cpu, memory, storage, pods)
	demand[scheduling.ResourceGPU] = gpu
	return demand
}

func batchQuota() scheduling.Demand { return cpuDemand(64_000, 256*gibibyte, tebibyte, 128) }

func trainingQuota() scheduling.Demand {
	return gpuDemand(32_000, 512*gibibyte, tebibyte, 64, 64)
}

// sumDemand and scaleDemand are unchecked on purpose. The domain's own
// arithmetic fails closed on overflow, which is what production needs; a
// fixture that overflowed would be a fixture bug, and the assertions it feeds
// would fail loudly either way.
func sumDemand(demands ...scheduling.Demand) scheduling.Demand {
	total := make(scheduling.Demand)
	for _, demand := range demands {
		for name, amount := range demand {
			total[name] += amount
		}
	}
	return total
}

func scaleDemand(demand scheduling.Demand, factor uint64) scheduling.Demand {
	scaled := make(scheduling.Demand, len(demand))
	for name, amount := range demand {
		scaled[name] = amount * factor
	}
	return scaled
}

// boundAssignment is what a bind records: one concrete host inside one zone,
// under the unconstrained topology every batch-cpu placement carries. It is not
// the zero assignment, so it also proves the recorded domains survive whatever
// encoding an adapter stores them in -- the assignment feeds the version
// digest, and a lost domain makes the record fail to revalidate on the way out.
func boundAssignment() scheduling.TopologyAssignment {
	return scheduling.TopologyAssignment{
		Domains: []scheduling.TopologyDomain{{Zone: "zone-a", Host: "node-1"}},
	}
}

// mismatchedAssignment is a valid assignment for a placement that never asked
// for one. It is refused because the constraint it names is not the constraint
// the placement sealed.
func mismatchedAssignment() scheduling.TopologyAssignment {
	return scheduling.TopologyAssignment{
		Constraint: scheduling.RequireTopology(scheduling.TopologyLevelZone),
		Domains:    []scheduling.TopologyDomain{{Zone: "zone-a"}},
	}
}

// requireFault is the assertion this suite exists for. Both the code and the
// reason are checked, because a caller that switches on faults.IsReason is what
// breaks when a factory swaps adapters, and "an error happened" would let two
// adapters disagree about which one while both stayed green.
func requireFault(tb testing.TB, err error, code faults.Code, reason, subject string) {
	tb.Helper()
	if err == nil {
		tb.Fatalf("%s: no error, want %s/%s", subject, code, reason)
	}
	if !faults.IsCode(err, code) || !faults.IsReason(err, reason) {
		tb.Fatalf("%s: fault is %s/%q, want %s/%q (%v)",
			subject, faults.CodeOf(err), faults.ReasonOf(err), code, reason, err)
	}
}

// requireDemand compares two demand vectors the way the domain does. An absent
// key and a zero amount mean the same thing here, and the two adapters produce
// different key sets for the same vector -- one sums in Go over the covered
// resource list, the other reads five columns back -- so a structural
// comparison would fail on records that are identical.
func requireDemand(tb testing.TB, got, want scheduling.Demand, subject string) {
	tb.Helper()
	if !got.Equal(want) {
		tb.Fatalf("%s = %v, want %v", subject, got, want)
	}
}
