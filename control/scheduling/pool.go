// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package scheduling

import (
	"go.mindclade.dev/control/orchestration"
)

// PoolKind names one fleet resource class. The seven pools are the ones the
// system design reference enumerates; they are a scheduling vocabulary, not a
// cluster object. Several pools share a single Kubernetes capacity domain,
// which is exactly why both concepts exist: the pool says what kind of work
// this is and how expensive it is to hold, and the domain says which reviewed
// queue the cluster will accept it into.
type PoolKind string

const (
	// PoolSearchCPU is MSA, template, and reference search. It is the upstream
	// resource class named by the GPU reservation rule: inference GPU is not
	// reserved while search is pending.
	PoolSearchCPU PoolKind = "search_cpu_high_memory"
	// PoolFeaturizationCPU is pairing, parsing, and feature construction.
	PoolFeaturizationCPU PoolKind = "featurization_cpu"
	// PoolChemistryCPU is CCD, conformer, and chemistry work.
	PoolChemistryCPU PoolKind = "chemistry_cpu"
	// PoolGPUInference is online and batch model execution.
	PoolGPUInference PoolKind = "gpu_inference"
	// PoolGPUTraining is multinode training.
	PoolGPUTraining PoolKind = "gpu_training"
	// PoolEvaluation is isolated CPU/GPU qualification.
	PoolEvaluation PoolKind = "evaluation"
	// PoolArtifactDataPlane is transfer, cache, and checkpoint traffic.
	PoolArtifactDataPlane PoolKind = "artifact_data_plane"
)

// pools is the closed set. Iteration order is fixed so every canonical form
// this package produces -- fingerprints, ledger keys, ordering -- is stable
// across processes and across Go map-iteration randomization.
var pools = [...]PoolKind{
	PoolSearchCPU,
	PoolFeaturizationCPU,
	PoolChemistryCPU,
	PoolGPUInference,
	PoolGPUTraining,
	PoolEvaluation,
	PoolArtifactDataPlane,
}

// Pools returns the seven fleet resource classes in canonical order.
func Pools() []PoolKind { return append([]PoolKind(nil), pools[:]...) }

func (kind PoolKind) Valid() bool {
	for _, candidate := range pools {
		if kind == candidate {
			return true
		}
	}
	return false
}

func (kind PoolKind) String() string { return string(kind) }

// Accelerated reports whether this pool is permitted to hold accelerator
// capacity at all. A demand naming nvidia.com/gpu from a pool that is not
// accelerated is a policy error, not a capacity shortfall: it means the caller
// classified the work into the wrong resource class.
//
// Evaluation is accelerated because the design reference calls it "isolated
// CPU/GPU qualification" -- it may run either, and which one it runs is decided
// by the demand, not by the pool.
func (kind PoolKind) Accelerated() bool {
	switch kind {
	case PoolGPUInference, PoolGPUTraining, PoolEvaluation:
		return true
	default:
		return false
	}
}

// ExpenseRank orders pools by how costly it is to hold their capacity idle.
// Larger is more expensive. The rank is what makes the "do not hold an
// expensive downstream resource while an upstream resource-class stage is
// incomplete" invariant checkable rather than a comment: placement compares
// the rank of the pool being requested against the rank of every pool whose
// upstream stage has not finished.
//
// The ordering is the hardware cost ordering, not a preference: an idle B200
// node wastes more than an idle high-memory search node, which wastes more
// than an idle transfer slot.
func (kind PoolKind) ExpenseRank() int {
	switch kind {
	case PoolGPUTraining:
		return 6
	case PoolGPUInference:
		return 5
	case PoolEvaluation:
		return 4
	case PoolSearchCPU:
		return 3
	case PoolChemistryCPU:
		return 2
	case PoolFeaturizationCPU:
		return 1
	case PoolArtifactDataPlane:
		return 0
	default:
		return -1
	}
}

// PoolForStage maps an orchestration stage kind onto its fleet resource class.
// Every one of the eleven stage kinds must map, and the mapping is total on
// purpose: an unmapped stage would otherwise be admitted into whatever pool the
// caller named, which is how a training stage ends up holding an inference GPU.
func PoolForStage(stage orchestration.StageKind) (PoolKind, error) {
	switch stage {
	case orchestration.StageIngestion, orchestration.StageCheckpointTransfer,
		orchestration.StageArtifactTransfer, orchestration.StageRollout:
		return PoolArtifactDataPlane, nil
	case orchestration.StageCurate, orchestration.StagePreprocess:
		return PoolFeaturizationCPU, nil
	case orchestration.StageReferenceBuild:
		return PoolSearchCPU, nil
	case orchestration.StageBatchInference:
		return PoolGPUInference, nil
	case orchestration.StageEvaluation:
		return PoolEvaluation, nil
	case orchestration.StageTraining:
		return PoolGPUTraining, nil
	case orchestration.StageSimulation:
		return PoolChemistryCPU, nil
	default:
		return "", invalid("stage_kind_unmapped", "stage kind has no fleet resource class", nil)
	}
}

// WorkloadClass is the public capacity discriminant. These three values are the
// entire admissible set: infra/kubernetes/platform/security/resources.yaml binds
// a ValidatingAdmissionPolicy with validationActions [Audit, Deny] that requires
// namespace, kueue.x-k8s.io/queue-name, and mindclade.dev/workload-class to form
// exactly one of three triples. A fourth class does not fail here and succeed in
// the cluster; it is rejected by the API server.
type WorkloadClass string

const (
	WorkloadClassBatchCPU     WorkloadClass = "batch-cpu"
	WorkloadClassTrainingH100 WorkloadClass = "training-h100"
	WorkloadClassTrainingB200 WorkloadClass = "training-b200"
)

var workloadClasses = [...]WorkloadClass{
	WorkloadClassBatchCPU,
	WorkloadClassTrainingH100,
	WorkloadClassTrainingB200,
}

// ResourceFlavor names a Kueue ResourceFlavor declared by
// infra/kubernetes/platform/kueue/resources.yaml.
type ResourceFlavor string

const (
	FlavorCPUGeneralOnDemand ResourceFlavor = "mindclade-cpu-general-ondemand"
	FlavorH100               ResourceFlavor = "mindclade-h100"
	FlavorB200               ResourceFlavor = "mindclade-b200"
)

func (flavor ResourceFlavor) String() string { return string(flavor) }

// TopologyAware reports whether this flavor carries a topologyName. Only the
// two GPU flavors reference mindclade-gpu-zone-host; the CPU flavor has no
// topology at all, so asking for a topology-constrained placement on it is
// invalid rather than merely unsatisfiable.
func (flavor ResourceFlavor) TopologyAware() bool {
	return flavor == FlavorH100 || flavor == FlavorB200
}

// Accelerator names the accelerator generation a placement asks for. It is the
// single free choice a caller has over the capacity domain: everything else in
// the triple follows from it.
type Accelerator string

const (
	AcceleratorNone Accelerator = "none"
	AcceleratorH100 Accelerator = "h100"
	AcceleratorB200 Accelerator = "b200"
)

func (accelerator Accelerator) Valid() bool {
	switch accelerator {
	case AcceleratorNone, AcceleratorH100, AcceleratorB200:
		return true
	default:
		return false
	}
}

// CapacityDomain is the namespace/queue-name/workload-class triple modelled as
// one value.
//
// The field is unexported and there is no composite literal path to a populated
// value, so the three components cannot drift apart. That is the whole point:
// the cluster policy denies a Job whose queue-name label does not match its
// namespace, and three independent string fields on a struct is precisely the
// shape that lets a refactor set two of them and forget the third.
type CapacityDomain struct {
	class WorkloadClass
}

// DomainFor returns the capacity domain for one workload class.
func DomainFor(class WorkloadClass) (CapacityDomain, error) {
	for _, candidate := range workloadClasses {
		if class == candidate {
			return CapacityDomain{class: class}, nil
		}
	}
	return CapacityDomain{}, invalid("workload_class_invalid", "workload class is not one of the three admissible capacity triples", nil)
}

// DomainForAccelerator returns the capacity domain that grants the requested
// accelerator. The batch-cpu ClusterQueue does not list nvidia.com/gpu among
// its covered resources, so accelerator capacity exists only in the two
// training-class queues.
func DomainForAccelerator(accelerator Accelerator) (CapacityDomain, error) {
	switch accelerator {
	case AcceleratorNone:
		return DomainFor(WorkloadClassBatchCPU)
	case AcceleratorH100:
		return DomainFor(WorkloadClassTrainingH100)
	case AcceleratorB200:
		return DomainFor(WorkloadClassTrainingB200)
	default:
		return CapacityDomain{}, invalid("accelerator_invalid", "accelerator is invalid", nil)
	}
}

// Domains returns the three capacity domains in canonical order.
func Domains() []CapacityDomain {
	domains := make([]CapacityDomain, 0, len(workloadClasses))
	for _, class := range workloadClasses {
		domains = append(domains, CapacityDomain{class: class})
	}
	return domains
}

func (domain CapacityDomain) IsZero() bool { return domain.class == "" }

func (domain CapacityDomain) Validate() error {
	if _, err := DomainFor(domain.class); err != nil {
		return err
	}
	return nil
}

// WorkloadClass returns the mindclade.dev/workload-class label value.
func (domain CapacityDomain) WorkloadClass() WorkloadClass { return domain.class }

// Namespace returns the only namespace this class may run in.
func (domain CapacityDomain) Namespace() string { return "mindclade-" + string(domain.class) }

// QueueName returns the kueue.x-k8s.io/queue-name label value. The LocalQueue
// is namespace-local and shares the namespace name, which is what the admission
// policy checks.
func (domain CapacityDomain) QueueName() string { return domain.Namespace() }

// ClusterQueue returns the ClusterQueue backing the namespace-local queue.
func (domain CapacityDomain) ClusterQueue() string { return domain.Namespace() }

// Flavor returns the single ResourceFlavor this domain's one ResourceGroup
// grants. There is exactly one flavor per domain, which is why a workload can
// never take CPU from one flavor and GPU from another.
func (domain CapacityDomain) Flavor() ResourceFlavor {
	switch domain.class {
	case WorkloadClassTrainingH100:
		return FlavorH100
	case WorkloadClassTrainingB200:
		return FlavorB200
	default:
		return FlavorCPUGeneralOnDemand
	}
}

// Accelerator returns the accelerator generation this domain grants.
func (domain CapacityDomain) Accelerator() Accelerator {
	switch domain.class {
	case WorkloadClassTrainingH100:
		return AcceleratorH100
	case WorkloadClassTrainingB200:
		return AcceleratorB200
	default:
		return AcceleratorNone
	}
}

// String returns the canonical triple for logs and fault messages.
func (domain CapacityDomain) String() string {
	return canonicalJoin(domain.Namespace(), domain.QueueName(), string(domain.class))
}

// MarshalText renders the domain as its workload class. The class is the whole
// identity; re-serializing all three components would invite a reader to trust
// a namespace that disagreed with it.
func (domain CapacityDomain) MarshalText() ([]byte, error) {
	if err := domain.Validate(); err != nil {
		return nil, err
	}
	return []byte(domain.class), nil
}

func (domain *CapacityDomain) UnmarshalText(value []byte) error {
	parsed, err := DomainFor(WorkloadClass(value))
	if err != nil {
		return err
	}
	*domain = parsed
	return nil
}

// AdmissibleDomains returns the capacity domains this pool may be placed into,
// in the order placement should consider them. A non-accelerated pool has
// exactly one; an accelerated pool has the two GPU-covered queues.
func (kind PoolKind) AdmissibleDomains() []CapacityDomain {
	if !kind.Valid() {
		return nil
	}
	if !kind.Accelerated() {
		batch, err := DomainFor(WorkloadClassBatchCPU)
		if err != nil {
			return nil
		}
		return []CapacityDomain{batch}
	}
	domains := make([]CapacityDomain, 0, len(workloadClasses))
	for _, class := range workloadClasses {
		domain, err := DomainFor(class)
		if err != nil {
			return nil
		}
		domains = append(domains, domain)
	}
	return domains
}

// Pool binds a resource class to the capacity domain a specific placement
// resolved it to. It is a derived value: nothing stores a Pool, and it is only
// ever produced by ResolvePool so the domain can never be set independently of
// the accelerator that justified it.
type Pool struct {
	Kind        PoolKind       `json:"kind"`
	Domain      CapacityDomain `json:"domain"`
	Flavor      ResourceFlavor `json:"flavor"`
	Accelerator Accelerator    `json:"accelerator"`
}

// ResolvePool binds a resource class to a capacity domain for one accelerator
// request. It fails closed: an accelerator asked for by a pool that may not
// hold accelerators is denied rather than silently downgraded to CPU.
func ResolvePool(kind PoolKind, accelerator Accelerator) (Pool, error) {
	if !kind.Valid() {
		return Pool{}, invalid("pool_kind_invalid", "pool kind is invalid", nil)
	}
	if !accelerator.Valid() {
		return Pool{}, invalid("accelerator_invalid", "accelerator is invalid", nil)
	}
	if accelerator != AcceleratorNone && !kind.Accelerated() {
		return Pool{}, denied("pool_not_accelerated", "resource class may not hold accelerator capacity")
	}
	domain, err := DomainForAccelerator(accelerator)
	if err != nil {
		return Pool{}, err
	}
	admissible := false
	for _, candidate := range kind.AdmissibleDomains() {
		if candidate == domain {
			admissible = true
			break
		}
	}
	if !admissible {
		return Pool{}, denied("pool_domain_not_admissible", "resource class may not be placed in this capacity domain")
	}
	return Pool{Kind: kind, Domain: domain, Flavor: domain.Flavor(), Accelerator: accelerator}, nil
}

func (pool Pool) Validate() error {
	resolved, err := ResolvePool(pool.Kind, pool.Accelerator)
	if err != nil {
		return err
	}
	if resolved != pool {
		return conflict("pool_binding_inconsistent", "pool binding does not match its capacity domain")
	}
	return nil
}

// canonical is the digest input for anything that seals a pool binding.
func (pool Pool) canonical() string {
	return canonicalJoin(string(pool.Kind), pool.Domain.String(), string(pool.Flavor), string(pool.Accelerator))
}
