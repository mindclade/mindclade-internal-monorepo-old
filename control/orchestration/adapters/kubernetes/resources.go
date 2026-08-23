// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package kubernetes

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"go.mindclade.dev/control/runtime_authority"
)

// ResourceGPU is the extended resource name the NVIDIA device plugin
// advertises. It appears on a container only when the ticket budgets GPU
// memory: an explicit zero on a CPU-only pod would make the pod request an
// extended resource no CPU node advertises, and it would sit Pending forever.
const ResourceGPU = corev1.ResourceName("nvidia.com/gpu")

// maximumQuantityBytes bounds a single resource amount. Kubernetes quantities
// are int64, and a uint64 budget above this would sign-flip on conversion --
// producing a negative request the API server reads as a malformed object
// rather than as the enormous number somebody meant.
const maximumQuantityBytes = uint64(1) << 62

// Requirements projects one execution ticket's budget onto a container's
// resource requirements.
//
// Requests and limits are the same list. The cluster's restricted-pods policy
// requires both, Kueue charges the request, and letting them diverge would let
// a workload be admitted for one amount and consume another -- which is the
// difference between a quota and a suggestion.
//
// devices is an argument rather than something derived from the budget. The
// ticket bounds GPU *memory*, not device count, and dividing one by an assumed
// per-device size would invent an allocation nobody authorised. What this
// function does enforce is that the two agree: devices without a GPU memory
// grant is capacity theft, and a GPU memory grant with no device is a pod that
// will never be scheduled.
func Requirements(budget runtime_authority.ExecutionBudget, devices uint32) (corev1.ResourceRequirements, error) {
	if err := budget.Validate(); err != nil {
		return corev1.ResourceRequirements{}, err
	}
	if (devices > 0) != (budget.GPUMemoryEstimateBytes > 0) {
		return corev1.ResourceRequirements{}, failedPrecondition("gpu_request_mismatch",
			"the requested device count and the ticket's gpu memory budget disagree")
	}
	// Shared memory is a tmpfs and Kubernetes charges tmpfs pages to the
	// container's memory cgroup. A shared-memory grant larger than the resident
	// bound therefore describes a pod that would be OOM-killed by its own
	// /dev/shm, which is worth refusing at the decision rather than debugging
	// from a kubelet event.
	if budget.SharedMemoryBytes > budget.ResidentMemoryBytes {
		return corev1.ResourceRequirements{}, failedPrecondition("shared_memory_exceeds_resident",
			"the ticket grants more shared memory than resident memory")
	}
	memory, err := quantity(budget.ResidentMemoryBytes, resource.BinarySI)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	ephemeral, err := ephemeralStorage(budget)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	list := corev1.ResourceList{
		corev1.ResourceCPU:              *resource.NewMilliQuantity(int64(budget.CPUMillis), resource.DecimalSI),
		corev1.ResourceMemory:           memory,
		corev1.ResourceEphemeralStorage: ephemeral,
	}
	if devices > 0 {
		list[ResourceGPU] = *resource.NewQuantity(int64(devices), resource.DecimalSI)
	}
	return corev1.ResourceRequirements{Requests: list.DeepCopy(), Limits: list.DeepCopy()}, nil
}

// ephemeralStorage is every local-disk consumer the ticket bounds, summed.
//
// Checkpoint staging and the telemetry spool land on the same filesystem as the
// working set, so a request covering only LocalDiskBytes would let a checkpoint
// evict the pod that wrote it. A ticket that grants no local disk at all is
// refused rather than defaulted: the admission policy requires a bounded
// ephemeral-storage request, and inventing one here would grant capacity the
// ticket withheld.
func ephemeralStorage(budget runtime_authority.ExecutionBudget) (resource.Quantity, error) {
	total := uint64(0)
	for _, amount := range []uint64{budget.LocalDiskBytes, budget.CheckpointStagingBytes, budget.TelemetrySpoolBytes} {
		if total > ^uint64(0)-amount {
			return resource.Quantity{}, invalid("local_disk_budget_overflow", "the ticket local-disk budgets sum beyond a uint64", nil)
		}
		total += amount
	}
	if total == 0 {
		return resource.Quantity{}, invalid("local_disk_budget_required", "the ticket grants no local disk for the workload", nil)
	}
	return quantity(total, resource.BinarySI)
}

func quantity(amount uint64, format resource.Format) (resource.Quantity, error) {
	if amount == 0 {
		return resource.Quantity{}, invalid("resource_amount_required", "resource amount must be positive", nil)
	}
	if amount > maximumQuantityBytes {
		return resource.Quantity{}, invalid("resource_amount_out_of_range", "resource amount is beyond the adapter bound", nil)
	}
	return *resource.NewQuantity(int64(amount), format), nil
}
