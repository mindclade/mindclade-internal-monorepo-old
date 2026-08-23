// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import (
	"sort"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/libs/go/identifiers"
)

// MaximumSnapshotDomains bounds a fleet snapshot. There are three capacity
// domains and there cannot be a fourth, so a snapshot carrying more than three
// ledgers is describing a cluster this package was not compiled against.
const MaximumSnapshotDomains = 3

// MaximumReplicas bounds one admission request. A placement covering more pods
// than this is not a scheduling decision, it is a mistake that would charge the
// whole queue against a single workload.
const MaximumReplicas = uint32(4_096)

// FleetSnapshot is the capacity and fairness view one admission decision is
// evaluated against. It is a value: admission never reads a store, so the same
// snapshot always yields the same verdict and a decision can be replayed from
// an audit record.
type FleetSnapshot struct {
	Epoch          uint64             `json:"epoch"`
	ObservedAt     time.Time          `json:"observed_at"`
	Allocatables   []Allocatable      `json:"allocatables,omitempty"`
	Shares         []FairShare        `json:"shares,omitempty"`
	TopologyDigest identifiers.Digest `json:"topology_digest"`
}

func (snapshot FleetSnapshot) Validate() error {
	if snapshot.Epoch == 0 {
		return invalid("snapshot_epoch_invalid", "fleet snapshot epoch is required", nil)
	}
	if snapshot.ObservedAt.IsZero() {
		return invalid("snapshot_observed_at_invalid", "fleet snapshot observation time is required", nil)
	}
	if len(snapshot.Allocatables) > MaximumSnapshotDomains || len(snapshot.Shares) > MaximumSnapshotDomains {
		return exhausted("snapshot_domain_bound", "fleet snapshot exceeds the capacity domain bound")
	}
	seenLedger := make(map[CapacityDomain]struct{}, len(snapshot.Allocatables))
	for _, allocatable := range snapshot.Allocatables {
		if err := allocatable.Validate(); err != nil {
			return err
		}
		if _, exists := seenLedger[allocatable.Domain]; exists {
			return invalid("snapshot_duplicate_ledger", "fleet snapshot repeats a capacity domain ledger", nil)
		}
		seenLedger[allocatable.Domain] = struct{}{}
	}
	seenShare := make(map[CapacityDomain]struct{}, len(snapshot.Shares))
	for _, share := range snapshot.Shares {
		if err := share.Validate(); err != nil {
			return err
		}
		if _, exists := seenShare[share.Domain]; exists {
			return invalid("snapshot_duplicate_share", "fleet snapshot repeats a fair-share view", nil)
		}
		seenShare[share.Domain] = struct{}{}
	}
	// A snapshot taken against a replaced topology describes hardware
	// boundaries that no longer mean what the constraint says they mean. The
	// Topology object is immutable, so a mismatch is a replacement, not drift.
	if !snapshot.TopologyDigest.IsZero() && !snapshot.TopologyDigest.Equal(TopologyFingerprint()) {
		return failedPrecondition("snapshot_topology_mismatch", "fleet snapshot was taken against a different topology contract")
	}
	return nil
}

func (snapshot FleetSnapshot) Clone() FleetSnapshot {
	allocatables := make([]Allocatable, 0, len(snapshot.Allocatables))
	for _, allocatable := range snapshot.Allocatables {
		allocatables = append(allocatables, allocatable.Clone())
	}
	shares := make([]FairShare, 0, len(snapshot.Shares))
	for _, share := range snapshot.Shares {
		shares = append(shares, share.Clone())
	}
	snapshot.Allocatables = allocatables
	snapshot.Shares = shares
	return snapshot
}

// Allocatable returns one domain's ledger. A missing ledger is not an empty
// ledger: it means the snapshot never observed that queue, and admitting
// against capacity nobody measured is exactly the fail-open this package exists
// to prevent.
func (snapshot FleetSnapshot) Allocatable(domain CapacityDomain) (Allocatable, error) {
	if err := domain.Validate(); err != nil {
		return Allocatable{}, err
	}
	for _, allocatable := range snapshot.Allocatables {
		if allocatable.Domain == domain {
			return allocatable.Clone(), nil
		}
	}
	return Allocatable{}, notFound("capacity_domain_unobserved", "fleet snapshot carries no ledger for the capacity domain")
}

// FairShare returns one domain's fairness view. An unobserved domain yields an
// empty view weighted only by the caller, which is correct: fairness cannot
// constrain a domain nobody has usage for.
func (snapshot FleetSnapshot) FairShare(domain CapacityDomain) (FairShare, error) {
	if err := domain.Validate(); err != nil {
		return FairShare{}, err
	}
	for _, share := range snapshot.Shares {
		if share.Domain == domain {
			return share.Clone(), nil
		}
	}
	allocatable, err := snapshot.Allocatable(domain)
	if err != nil {
		return FairShare{}, err
	}
	return FairShare{Domain: domain, Capacity: allocatable.Nominal.Clone()}, nil
}

func (snapshot FleetSnapshot) canonical() string {
	values := make([]string, 0, len(snapshot.Allocatables)+len(snapshot.Shares)+2)
	values = append(values, snapshot.TopologyDigest.String(), snapshot.ObservedAt.UTC().Format(time.RFC3339Nano))
	ledgers := append([]Allocatable(nil), snapshot.Allocatables...)
	sort.Slice(ledgers, func(left, right int) bool {
		return ledgers[left].Domain.String() < ledgers[right].Domain.String()
	})
	for _, ledger := range ledgers {
		values = append(values, ledger.canonical())
	}
	shares := append([]FairShare(nil), snapshot.Shares...)
	sort.Slice(shares, func(left, right int) bool {
		return shares[left].Domain.String() < shares[right].Domain.String()
	})
	for _, share := range shares {
		values = append(values, share.canonical())
	}
	return canonicalJoin(values...)
}

// Fingerprint seals a snapshot so a decision can name the exact fleet state it
// was taken against.
func (snapshot FleetSnapshot) Fingerprint() identifiers.Digest {
	return identifiers.SHA256String(canonicalJoin("mindclade-fleet-snapshot-v1", snapshot.canonical()))
}

// AdmissionRequest is a fleet admission question: may this tenant hold this
// much of this resource class right now?
//
// It is deliberately unrelated to control/admission, which decides whether a
// gateway caller may spend tokens. This one decides whether the fleet has room.
type AdmissionRequest struct {
	WorkloadID  identifiers.ID          `json:"workload_id"`
	Tenant      string                  `json:"tenant"`
	Workspace   string                  `json:"workspace"`
	StageKind   orchestration.StageKind `json:"stage_kind"`
	Pool        PoolKind                `json:"pool"`
	Accelerator Accelerator             `json:"accelerator"`
	Priority    PriorityClass           `json:"priority"`
	Demand      Demand                  `json:"demand"`
	Replicas    uint32                  `json:"replicas"`
	Topology    TopologyConstraint      `json:"topology"`
	Upstream    []UpstreamResourceStage `json:"upstream,omitempty"`
}

func (request AdmissionRequest) Validate() error {
	if err := validateID(request.WorkloadID, "workload", "workload_id"); err != nil {
		return err
	}
	if err := validateName(request.Tenant, "tenant"); err != nil {
		return err
	}
	if err := validateName(request.Workspace, "workspace"); err != nil {
		return err
	}
	if !request.StageKind.Valid() {
		return invalid("stage_kind_invalid", "stage kind is invalid", nil)
	}
	// The pool a caller names must be the pool the stage kind maps to. A caller
	// that could pick its own resource class could route a training stage into
	// the artifact plane and hold GPU capacity it was never admitted for.
	expected, err := PoolForStage(request.StageKind)
	if err != nil {
		return err
	}
	if request.Pool != expected {
		return conflict("pool_stage_mismatch", "requested resource class does not match the stage kind")
	}
	if !request.Priority.Valid() {
		return invalid("priority_class_invalid", "priority class is not one of the two declared classes", nil)
	}
	if request.Replicas == 0 || request.Replicas > MaximumReplicas {
		return invalid("replicas_out_of_range", "replica count is outside bounds", nil)
	}
	if err := request.Demand.Validate(true); err != nil {
		return err
	}
	if request.Demand[ResourcePods] == 0 {
		return invalid("demand_pods_required", "a placement must charge at least one pod", nil)
	}
	if err := request.Topology.Validate(); err != nil {
		return err
	}
	if !request.Accelerator.Valid() {
		return invalid("accelerator_invalid", "accelerator is invalid", nil)
	}
	return validateUpstream(request.Upstream)
}

// TotalDemand returns the per-replica demand scaled by the replica count. The
// queue charges every pod, so admission must too.
func (request AdmissionRequest) TotalDemand() (Demand, error) {
	return request.Demand.Scale(uint64(request.Replicas))
}

// AdmissionVerdict is the outcome of a fleet admission decision. Its zero value
// is a rejection, so a caller that ignores an error still cannot read an
// admitted verdict out of it.
type AdmissionVerdict struct {
	Admitted          bool               `json:"admitted"`
	Pool              Pool               `json:"pool"`
	Total             Demand             `json:"total"`
	QueuePriority     int32              `json:"queue_priority"`
	FairSharePosition uint64             `json:"fair_share_position"`
	Epoch             uint64             `json:"epoch"`
	SnapshotDigest    identifiers.Digest `json:"snapshot_digest"`
}

func (verdict AdmissionVerdict) Validate() error {
	if !verdict.Admitted {
		return failedPrecondition("verdict_not_admitted", "admission verdict does not admit the workload")
	}
	if err := verdict.Pool.Validate(); err != nil {
		return err
	}
	if err := verdict.Total.ValidateForDomain(verdict.Pool.Domain, true); err != nil {
		return err
	}
	if verdict.Epoch == 0 || !verdict.SnapshotDigest.Valid() {
		return invalid("verdict_provenance_invalid", "admission verdict does not name the snapshot it was taken against", nil)
	}
	if verdict.FairSharePosition > ShareScale {
		return invalid("verdict_fair_share_invalid", "admission verdict fair-share position is outside bounds", nil)
	}
	return nil
}

// Admit is the fleet admission decision: quota, then fair share, then pool
// availability. Every rejection is a fault, not a boolean, because the three
// reasons demand different operator responses -- add nodes, wait for a peer, or
// activate a held queue -- and collapsing them into false loses that.
//
// It is a pure function of the snapshot and the request. Nothing here reads a
// clock, a store, or the cluster.
func (snapshot FleetSnapshot) Admit(request AdmissionRequest, now time.Time) (AdmissionVerdict, error) {
	if err := snapshot.Validate(); err != nil {
		return AdmissionVerdict{}, err
	}
	if err := request.Validate(); err != nil {
		return AdmissionVerdict{}, err
	}
	if now.IsZero() {
		return AdmissionVerdict{}, invalid("decision_time_invalid", "admission decision time is required", nil)
	}
	if snapshot.ObservedAt.After(now.Round(0).UTC()) {
		return AdmissionVerdict{}, failedPrecondition("snapshot_from_the_future", "fleet snapshot was observed after the decision time")
	}
	pool, err := ResolvePool(request.Pool, request.Accelerator)
	if err != nil {
		return AdmissionVerdict{}, err
	}
	if err := request.Topology.ValidateForFlavor(pool.Flavor); err != nil {
		return AdmissionVerdict{}, err
	}
	total, err := request.TotalDemand()
	if err != nil {
		return AdmissionVerdict{}, err
	}
	if err := total.ValidateForDomain(pool.Domain, true); err != nil {
		return AdmissionVerdict{}, err
	}

	allocatable, err := snapshot.Allocatable(pool.Domain)
	if err != nil {
		return AdmissionVerdict{}, err
	}
	// Every ClusterQueue in the manifest ships with nominalQuota "0" and
	// stopPolicy Hold. A zero-quota domain is held, not merely full: admitting
	// against it would produce workloads that sit unadmitted in Kueue forever
	// while this package's ledger believed capacity was in use.
	if allocatable.Nominal.IsZero() {
		return AdmissionVerdict{}, failedPrecondition("capacity_domain_inactive", "capacity domain grants no nominal quota")
	}
	admits, err := allocatable.Admits(total)
	if err != nil {
		return AdmissionVerdict{}, err
	}
	if !admits {
		return AdmissionVerdict{}, exhausted("capacity_exhausted", "capacity domain cannot satisfy the demand")
	}

	share, err := snapshot.FairShare(pool.Domain)
	if err != nil {
		return AdmissionVerdict{}, err
	}
	position, err := share.Position(request.Tenant)
	if err != nil {
		return AdmissionVerdict{}, err
	}
	withinShare, err := share.Admits(request.Tenant, total)
	if err != nil {
		return AdmissionVerdict{}, err
	}
	if !withinShare {
		// Over-share use is allowed only while nobody else is starved. A tenant
		// borrowing idle capacity is efficient; a tenant borrowing capacity a
		// starved peer is waiting for is the unfairness fair share exists to
		// stop, and there are no cohorts to reclaim it from afterwards.
		contended, err := share.Contended(request.Tenant)
		if err != nil {
			return AdmissionVerdict{}, err
		}
		if contended {
			return AdmissionVerdict{}, exhausted("fair_share_exhausted", "tenant is over its weighted share while a peer is below its own")
		}
	}

	priority, err := QueuePriority(request.Priority, position)
	if err != nil {
		return AdmissionVerdict{}, err
	}
	verdict := AdmissionVerdict{
		Admitted:          true,
		Pool:              pool,
		Total:             total,
		QueuePriority:     priority,
		FairSharePosition: position,
		Epoch:             snapshot.Epoch,
		SnapshotDigest:    snapshot.Fingerprint(),
	}
	if err := verdict.Validate(); err != nil {
		return AdmissionVerdict{}, err
	}
	return verdict, nil
}
