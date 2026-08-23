// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import (
	"strconv"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/libs/go/identifiers"
)

// MaximumUpstreamStages bounds the dependency evidence one placement carries.
// It mirrors the workflow stage bound: a placement can name at most as many
// upstream stages as a workflow can contain. It is spelled out here rather than
// imported so this package's own bound holds even if the workflow bound is
// later relaxed -- a placement walks this list on every decision.
const MaximumUpstreamStages = 4096

// UpstreamResourceStage is one upstream stage a placement depends on, and
// whether it has finished.
//
// Completion is supplied by the caller rather than read from a store because
// this package is synchronous pure policy: it decides, it does not discover.
// The caller that claimed the work item already holds the run state.
type UpstreamResourceStage struct {
	StageID  string                  `json:"stage_id"`
	Kind     orchestration.StageKind `json:"kind"`
	Complete bool                    `json:"complete"`
}

func (stage UpstreamResourceStage) Validate() error {
	if _, err := identifiers.ParseID(stage.StageID); err != nil {
		return invalid("upstream_stage_id_invalid", "upstream stage id must be canonical", err)
	}
	if !stage.Kind.Valid() {
		return invalid("upstream_stage_kind_invalid", "upstream stage kind is invalid", nil)
	}
	if _, err := PoolForStage(stage.Kind); err != nil {
		return err
	}
	return nil
}

func validateUpstream(stages []UpstreamResourceStage) error {
	if len(stages) > MaximumUpstreamStages {
		return exhausted("upstream_stage_bound", "upstream stage list exceeds its bound")
	}
	seen := make(map[string]struct{}, len(stages))
	for _, stage := range stages {
		if err := stage.Validate(); err != nil {
			return err
		}
		if _, exists := seen[stage.StageID]; exists {
			return invalid("upstream_stage_duplicate", "upstream stage list repeats a stage", nil)
		}
		seen[stage.StageID] = struct{}{}
	}
	return nil
}

// EnforceUpstreamResourceClass implements the system-design invariant that
// scheduling must not hold an expensive downstream resource while an upstream
// resource-class stage is incomplete.
//
// The comparison is by expense rank across resource classes. An upstream stage
// in the SAME class is not a violation -- that is ordinary sequencing inside one
// pool, and the capacity is charged once either way. A downstream class that is
// cheaper than the pending upstream one is also fine: holding a transfer slot
// while training finishes wastes nothing worth protecting.
//
// What this stops is the expensive case: a GPU sitting idle, admitted and
// charged, waiting on a CPU search that has not produced its inputs yet.
func EnforceUpstreamResourceClass(pool PoolKind, upstream []UpstreamResourceStage) error {
	if !pool.Valid() {
		return invalid("pool_kind_invalid", "pool kind is invalid", nil)
	}
	if err := validateUpstream(upstream); err != nil {
		return err
	}
	rank := pool.ExpenseRank()
	for _, stage := range upstream {
		if stage.Complete {
			continue
		}
		upstreamPool, err := PoolForStage(stage.Kind)
		if err != nil {
			return err
		}
		if upstreamPool == pool {
			continue
		}
		if rank > upstreamPool.ExpenseRank() {
			return failedPrecondition(
				"upstream_resource_class_incomplete",
				"a more expensive downstream resource class cannot be held while an upstream resource-class stage is incomplete",
			)
		}
	}
	return nil
}

// EnforceGPUReservationRule implements the GPU reservation rule: no inference
// GPU or model slot is reserved while MSA, template, or reference search is
// pending. CPU/high-memory search and GPU model execution are separately
// scheduled resource classes and are never held together.
//
// This overlaps EnforceUpstreamResourceClass today, because every accelerated
// pool outranks the search pool. It is a separate, separately named check on
// purpose: the rank table is a tuning surface, and this rule is not. If a future
// change reorders the ranks, the generic invariant may stop covering this case
// and this one still will.
func EnforceGPUReservationRule(pool PoolKind, accelerator Accelerator, upstream []UpstreamResourceStage) error {
	if !pool.Valid() {
		return invalid("pool_kind_invalid", "pool kind is invalid", nil)
	}
	if !accelerator.Valid() {
		return invalid("accelerator_invalid", "accelerator is invalid", nil)
	}
	if err := validateUpstream(upstream); err != nil {
		return err
	}
	holdsAccelerator := accelerator != AcceleratorNone || pool == PoolGPUInference
	if !holdsAccelerator {
		return nil
	}
	for _, stage := range upstream {
		if stage.Complete {
			continue
		}
		upstreamPool, err := PoolForStage(stage.Kind)
		if err != nil {
			return err
		}
		if upstreamPool == PoolSearchCPU {
			return failedPrecondition(
				"gpu_reserved_before_search",
				"an inference GPU or model slot cannot be reserved while reference search is pending",
			)
		}
	}
	return nil
}

// PlacementRequest is one placement decision's input: the fleet admission
// question plus the run coordinates the resulting cluster object is named for.
type PlacementRequest struct {
	Admission AdmissionRequest `json:"admission"`
	RunID     string           `json:"run_id"`
	StageID   string           `json:"stage_id"`
	Attempt   uint32           `json:"attempt"`
}

func (request PlacementRequest) Validate() error {
	if err := request.Admission.Validate(); err != nil {
		return err
	}
	for name, value := range map[string]string{"run_id": request.RunID, "stage_id": request.StageID} {
		if _, err := identifiers.ParseID(value); err != nil {
			return invalid(name+"_invalid", name+" must be canonical", err)
		}
	}
	if request.Attempt == 0 {
		return invalid("attempt_invalid", "placement attempt must be positive", nil)
	}
	for _, stage := range request.Admission.Upstream {
		if stage.StageID == request.StageID {
			return invalid("upstream_self_reference", "a placement cannot depend on its own stage", nil)
		}
	}
	return nil
}

// Placement is the sealed decision: which capacity triple, which flavor, which
// topology, at which priority, for how much.
//
// It is the only value this package hands to an adapter. An adapter cannot
// choose a queue name, because the queue name is not an adapter input -- it is
// derived from the capacity domain that admission resolved.
type Placement struct {
	WorkloadID     identifiers.ID     `json:"workload_id"`
	RunID          string             `json:"run_id"`
	StageID        string             `json:"stage_id"`
	Attempt        uint32             `json:"attempt"`
	Tenant         string             `json:"tenant"`
	Workspace      string             `json:"workspace"`
	Pool           Pool               `json:"pool"`
	Replicas       uint32             `json:"replicas"`
	PerReplica     Demand             `json:"per_replica"`
	Total          Demand             `json:"total"`
	Priority       PriorityClass      `json:"priority"`
	QueuePriority  int32              `json:"queue_priority"`
	Topology       TopologyConstraint `json:"topology"`
	TopologyDigest identifiers.Digest `json:"topology_digest"`
	Epoch          uint64             `json:"epoch"`
	SnapshotDigest identifiers.Digest `json:"snapshot_digest"`
	DecidedAt      time.Time          `json:"decided_at"`
	Digest         identifiers.Digest `json:"digest"`
}

func (placement Placement) Validate() error {
	if err := validateID(placement.WorkloadID, "workload", "workload_id"); err != nil {
		return err
	}
	for name, value := range map[string]string{"run_id": placement.RunID, "stage_id": placement.StageID} {
		if _, err := identifiers.ParseID(value); err != nil {
			return invalid(name+"_invalid", name+" must be canonical", err)
		}
	}
	if placement.Attempt == 0 {
		return invalid("attempt_invalid", "placement attempt must be positive", nil)
	}
	if err := validateName(placement.Tenant, "tenant"); err != nil {
		return err
	}
	if err := validateName(placement.Workspace, "workspace"); err != nil {
		return err
	}
	if err := placement.Pool.Validate(); err != nil {
		return err
	}
	if placement.Replicas == 0 || placement.Replicas > MaximumReplicas {
		return invalid("replicas_out_of_range", "replica count is outside bounds", nil)
	}
	if err := placement.PerReplica.ValidateForDomain(placement.Pool.Domain, true); err != nil {
		return err
	}
	if err := placement.Total.ValidateForDomain(placement.Pool.Domain, true); err != nil {
		return err
	}
	expectedTotal, err := placement.PerReplica.Scale(uint64(placement.Replicas))
	if err != nil {
		return err
	}
	if !expectedTotal.Equal(placement.Total) {
		return conflict("placement_total_mismatch", "placement total is not its per-replica demand scaled by the replica count")
	}
	if !placement.Priority.Valid() {
		return invalid("priority_class_invalid", "priority class is not one of the two declared classes", nil)
	}
	if err := placement.Topology.ValidateForFlavor(placement.Pool.Flavor); err != nil {
		return err
	}
	if !placement.TopologyDigest.Equal(placement.Topology.Fingerprint()) {
		return conflict("placement_topology_digest_mismatch", "placement topology digest does not seal its constraint")
	}
	if placement.Epoch == 0 || !placement.SnapshotDigest.Valid() {
		return invalid("placement_provenance_invalid", "placement does not name the snapshot it was taken against", nil)
	}
	if placement.DecidedAt.IsZero() {
		return invalid("placement_decided_at_invalid", "placement decision time is required", nil)
	}
	if !placement.Digest.Equal(placementDigest(placement)) {
		return conflict("placement_digest_mismatch", "placement digest does not seal its content")
	}
	return nil
}

func (placement Placement) clone() Placement {
	placement.PerReplica = placement.PerReplica.Clone()
	placement.Total = placement.Total.Clone()
	return placement
}

// QueueLabels returns the exact label set the capacity admission policy checks.
// It is produced here, from the sealed placement, so no adapter has to
// reassemble the triple and no adapter can get it wrong.
func QueueLabels(placement Placement) (map[string]string, error) {
	if err := placement.Validate(); err != nil {
		return nil, err
	}
	return map[string]string{
		"kueue.x-k8s.io/queue-name":    placement.Pool.Domain.QueueName(),
		"mindclade.dev/workload-class": string(placement.Pool.Domain.WorkloadClass()),
	}, nil
}

func placementDigest(placement Placement) identifiers.Digest {
	return identifiers.SHA256String(canonicalJoin(
		"mindclade-placement-v1",
		placement.WorkloadID.String(), placement.RunID, placement.StageID,
		strconv.FormatUint(uint64(placement.Attempt), 10),
		placement.Tenant, placement.Workspace,
		placement.Pool.canonical(),
		strconv.FormatUint(uint64(placement.Replicas), 10),
		placement.PerReplica.canonical(), placement.Total.canonical(),
		string(placement.Priority), strconv.FormatInt(int64(placement.QueuePriority), 10),
		placement.Topology.canonical(), placement.TopologyDigest.String(),
		strconv.FormatUint(placement.Epoch, 10), placement.SnapshotDigest.String(),
		placement.DecidedAt.UTC().Format(time.RFC3339Nano),
	))
}

// Place resolves one placement against a fleet snapshot.
//
// Order matters. Both dependency invariants are enforced BEFORE admission
// touches the capacity ledger, because the whole point of the rules is that the
// expensive resource is never reserved -- checking them after reserving would
// have already done the thing they forbid, and would leave the ledger to be
// unwound by whatever ran next.
func (snapshot FleetSnapshot) Place(request PlacementRequest, now time.Time) (Placement, error) {
	if err := request.Validate(); err != nil {
		return Placement{}, err
	}
	if now.IsZero() {
		return Placement{}, invalid("decision_time_invalid", "placement decision time is required", nil)
	}
	admission := request.Admission
	if err := EnforceGPUReservationRule(admission.Pool, admission.Accelerator, admission.Upstream); err != nil {
		return Placement{}, err
	}
	if err := EnforceUpstreamResourceClass(admission.Pool, admission.Upstream); err != nil {
		return Placement{}, err
	}
	verdict, err := snapshot.Admit(admission, now)
	if err != nil {
		return Placement{}, err
	}
	decidedAt := now.Round(0).UTC()
	placement := Placement{
		WorkloadID:     admission.WorkloadID,
		RunID:          request.RunID,
		StageID:        request.StageID,
		Attempt:        request.Attempt,
		Tenant:         admission.Tenant,
		Workspace:      admission.Workspace,
		Pool:           verdict.Pool,
		Replicas:       admission.Replicas,
		PerReplica:     admission.Demand.Clone(),
		Total:          verdict.Total,
		Priority:       admission.Priority,
		QueuePriority:  verdict.QueuePriority,
		Topology:       admission.Topology,
		TopologyDigest: admission.Topology.Fingerprint(),
		Epoch:          verdict.Epoch,
		SnapshotDigest: verdict.SnapshotDigest,
		DecidedAt:      decidedAt,
	}
	placement.Digest = placementDigest(placement)
	if err := placement.Validate(); err != nil {
		return Placement{}, err
	}
	return placement, nil
}
