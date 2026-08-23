// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package kubernetes

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/scheduling"
	schedulingkueue "go.mindclade.dev/control/scheduling/adapters/kueue"
	"go.mindclade.dev/libs/go/kubernetes"
)

// JobUIDLabel is the label Kueue copies onto the Workload it creates for a
// batch object. It is the only stable link between a JobSet and its Workload:
// Kueue derives the Workload name from the owner's name and a hash it owns, so
// a control plane that tried to reconstruct that name would be reimplementing
// an internal Kueue detail and would silently stop matching on upgrade.
const JobUIDLabel = "kueue.x-k8s.io/job-uid"

// MaximumListedWorkloads bounds the admission lookup. One JobSet has exactly
// one Workload; anything beyond a handful means the selector matched something
// unexpected, and paging through it would turn a mislabelled cluster into an
// unbounded read.
const MaximumListedWorkloads = 8

// WorkloadListGVK is the list kind the admission lookup requests.
var WorkloadListGVK = schedulingkueue.GroupVersion.WithKind("WorkloadList")

// Domain resolves the capacity triple a workload runs in.
//
// The envelope's resource class IS the workload class. That is the whole
// mapping, and it is deliberately not a lookup table: the cluster's
// ValidatingAdmissionPolicy denies any object whose namespace, queue-name
// label, and workload-class label are not one of three exact triples, so a
// resource class that named something else would produce an object the API
// server refuses. CapacityDomain is reused from control/scheduling rather than
// restated here for the same reason its own doc gives -- three independent
// strings is precisely the shape that gets two of them right.
func Domain(envelope orchestration.WorkloadEnvelope) (scheduling.CapacityDomain, error) {
	domain, err := scheduling.DomainFor(scheduling.WorkloadClass(envelope.ResourceClass))
	if err != nil {
		return scheduling.CapacityDomain{}, err
	}
	// An accelerator budget in a CPU domain is a workload that will never be
	// placed: the batch-cpu ClusterQueue does not cover nvidia.com/gpu at all.
	if envelope.ExecutionTicket.Claims.Budget.GPUMemoryEstimateBytes > 0 && domain.Accelerator() == scheduling.AcceleratorNone {
		return scheduling.CapacityDomain{}, failedPrecondition("accelerator_domain_mismatch",
			"the ticket budgets gpu memory in a capacity domain that grants no accelerator")
	}
	return domain, nil
}

// Admission is what Kueue says about one workload's place in the queue.
//
// Evicted is tracked because Kueue can evict for reasons this package does not
// author -- a preemption policy change, a queue stop, a node drain -- and an
// evicted workload that still looks admitted would be reported as starting
// while nothing is going to start.
type Admission struct {
	Known         bool
	QuotaReserved bool
	Admitted      bool
	Evicted       bool
	Finished      bool
}

// Admissions reads the Kueue admission status for one JobSet.
//
// It is a separate collaborator rather than a method on the launcher because it
// is optional. A deployment that has not enabled Kueue, or a component test
// that does not model it, still needs Launch, Observe, and Cancel to work; what
// it loses is only the distinction between "queued with no quota" and "quota
// granted, not yet unsuspended".
type Admissions interface {
	Admission(ctx context.Context, namespace string, jobUID types.UID) (Admission, error)
}

// KueueAdmissions reads Workload objects through a controller-runtime client.
type KueueAdmissions struct {
	Client crclient.Client
}

var _ Admissions = KueueAdmissions{}

// Admission finds the Workload Kueue created for a JobSet and projects it.
//
// A JobSet with no Workload yet is not an error: Kueue's webhook creates the
// Workload asynchronously, so the window between apply and admission is normal
// and must read as "nothing known" rather than as a missing object.
func (reader KueueAdmissions) Admission(ctx context.Context, namespace string, jobUID types.UID) (Admission, error) {
	if ctx == nil {
		return Admission{}, invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return Admission{}, canceled(err)
	}
	if reader.Client == nil {
		return Admission{}, unavailable("client_unavailable", "kubernetes client is unavailable", nil)
	}
	if namespace == "" || jobUID == "" {
		return Admission{}, invalid("workload_selector_incomplete", "an admission lookup requires a namespace and an owner uid", nil)
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(WorkloadListGVK)
	err := reader.Client.List(ctx, list,
		crclient.InNamespace(namespace),
		crclient.MatchingLabels{JobUIDLabel: string(jobUID)},
		crclient.Limit(MaximumListedWorkloads))
	if err != nil {
		reference := kubernetes.ObjectReference{APIVersion: schedulingkueue.GroupVersion.String(), Kind: "Workload", Namespace: namespace}
		return Admission{}, kubernetes.QualifyObject(ctx, err, operation+".Admission", reference, nil)
	}
	if len(list.Items) == 0 {
		return Admission{}, nil
	}
	if len(list.Items) > 1 {
		// Two Workloads for one owner uid means something other than Kueue is
		// writing them. Picking one would make admission non-deterministic.
		return Admission{}, failedPrecondition("workload_ambiguous", "more than one kueue workload claims this jobset")
	}
	return ProjectAdmission(&list.Items[0])
}

// ProjectAdmission reads one Kueue Workload's status conditions.
func ProjectAdmission(object *unstructured.Unstructured) (Admission, error) {
	if object == nil {
		return Admission{}, invalid("object_nil", "workload object is required", nil)
	}
	conditions, err := parseConditions(object)
	if err != nil {
		return Admission{}, err
	}
	return Admission{
		Known:         true,
		QuotaReserved: conditions[schedulingkueue.ConditionQuotaReserved],
		Admitted:      conditions[schedulingkueue.ConditionAdmitted],
		Evicted:       conditions[schedulingkueue.ConditionEvicted],
		Finished:      conditions[schedulingkueue.ConditionFinished],
	}, nil
}

// Placed reports whether Kueue has granted this workload capacity and has not
// taken it back. It is the condition that separates an attempt that is starting
// from one that is still waiting for quota.
func (admission Admission) Placed() bool {
	return admission.Known && admission.QuotaReserved && admission.Admitted && !admission.Evicted && !admission.Finished
}
