// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package scheduling owns fleet capacity, queues, priority, fair share,
// placement, topology, reservations, and preemption policy.
//
// It owns the decision and nothing else. Node-local admission and GPU tensor
// batching stay in the Rust data plane; durable providers, transactions, and
// process lifecycle stay in services/control_plane. This package is
// synchronous pure domain policy: it starts no goroutine, opens no socket,
// reads no clock it was not handed, and reaches the durable store only through
// the Repository interface in repository.go.
//
// # The cluster is the authority, not this package
//
// Three facts from infra/kubernetes constrain everything here, and none of them
// is a preference this package may relax:
//
//   - A workload's namespace, kueue.x-k8s.io/queue-name label, and
//     mindclade.dev/workload-class label must form exactly one of three
//     triples. A ValidatingAdmissionPolicy with validationActions [Audit, Deny]
//     enforces it at the API server. CapacityDomain therefore models the triple
//     as ONE value derived from one discriminant, and there is no path to a
//     populated value whose three components disagree.
//   - All five covered resources -- cpu, memory, ephemeral-storage,
//     nvidia.com/gpu, pods -- live in ONE ResourceGroup, so a workload takes
//     its flavor selectors for every resource from one flavor. Demand is a
//     single coherent vector, admitted or refused as a unit, never piecewise.
//     The batch-cpu queue omits nvidia.com/gpu entirely, so a GPU demand there
//     is denied rather than queued.
//   - Kueue preemption is disabled everywhere: reclaimWithinCohort Never,
//     withinClusterQueue Never, and no cohort exists. preemption.go is
//     therefore Go-side policy only -- victim selection and ordering, surfaced
//     as eviction and requeue. It cannot delegate reclamation to the cluster,
//     and mindclade-batch declares preemptionPolicy Never, so batch work may
//     never preempt anything.
//
// # Two invariants with names
//
// Scheduling must not hold an expensive downstream resource while an upstream
// resource-class stage is incomplete (EnforceUpstreamResourceClass), and no
// inference GPU or model slot is reserved while MSA, template, or reference
// search is pending (EnforceGPUReservationRule). Both run before admission
// touches the capacity ledger, because checking them afterwards would already
// have done the thing they forbid.
//
// # Shape
//
// Aggregates are immutable values sealed by resourceversion.Version: validate,
// check the source state, check timing, clone, mutate, re-seal, re-validate.
// All resource arithmetic is integer and overflow-checked. Every queue, map,
// and list is bounded. Errors are structured faults, IDs are canonical
// identifiers, and every decision is a pure function of a snapshot so it can be
// replayed from an audit record.
package scheduling
