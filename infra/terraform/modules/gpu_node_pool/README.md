# GKE accelerator node-pool module

This module adds one single-zone accelerator pool to an existing regional GKE
cluster. The admitted profiles are deliberately narrow: A3 Mega H100 and A3 Ultra
H200 eight-GPU nodes used by the NOVA execution-plan contract. Each profile fixes
the machine type, accelerator type/count, and expected fabric. Nodes use a dedicated
keyless service account, GKE metadata, Shielded VM, managed NVIDIA drivers,
accelerator networking, topology-aware kubelet settings, a mandatory GPU taint,
private networking, bounded autoscaling, guarded drain/upgrade behavior, and
provider/Terraform deletion protection.

Caller-supplied node labels and taints are checked against the Kubernetes API
grammar before provider planning. A qualified key has an optional lowercase DNS
subdomain prefix of at most 253 characters, followed by `/` and a required name of
at most 63 characters. Values may be empty; non-empty values must be at most 63
characters, begin and end with an alphanumeric character, and contain only
alphanumerics, `-`, `_`, or `.`. The module-owned identity labels and
`nvidia.com/gpu=present:NoSchedule` taint still take precedence. The final merged
node-label map must also stay below GKE's 1,024-character aggregate limit.

On-demand, reservation, Spot, Flex Start, and queued provisioning are modeled
explicitly. Standard on-demand capacity is accepted for H100 but rejected for A3
Ultra H200, which must use Spot, Flex Start, queued provisioning, or an approved
reservation. Queued provisioning enables both provider-required queued and Flex Start
settings and has a seven-day maximum lifetime. Standalone Flex Start is preview-only
and requires an explicit approval input. Both modes must scale from zero and disable
MIG compact placement; reservations require an exact reservation name. Ordinary
compact pools default to unavailable-first upgrades to avoid assuming spare accelerator
capacity. Flex Start and queued provisioning explicitly opt out of reservations.
Boot-disk CMEK selects `pd-ssd` for H100; it is rejected for H200 because that profile
requires Hyperdisk and GKE does not currently support boot-disk CMEK on Hyperdisk.
Capacity, quota, zone availability, Dynamic Workload
Scheduler, Compact Placement, driver compatibility, GPUDirect/RDMA components,
Kueue/JobSet policy, workload checkpointing, and artifact durability remain
environment and Kubernetes-layer responsibilities.

This module is an alternative capacity authority to the GKE `ComputeClass`
components under `infra/gpu`; it is not layered underneath them. An environment
must never let both mechanisms manage the same GPU profile and zone. The current
qualification overlays choose `ComputeClass`, while this module remains available
for a separately reviewed fixed-pool environment.

The node configuration enables GKE image streaming through `gcfs_config`. Before
using the module, the environment must enable the Container File System API,
provide Private Google Access for private nodes, grant the node service account
Artifact Registry Reader, and grant Service Usage Consumer in the image project
when images and nodes use different projects. Runtime images must be eligible,
immutable Artifact Registry digest references; enabling GCFS does not make
arbitrary registries streamable.

Before rollout, verify the current GKE accelerator matrix and regional inventory,
reserve quota/capacity, inspect a saved plan, and qualify each exact image/driver/
machine/fabric combination with CUDA, collective, topology, interruption, drain,
checkpoint/restart, and cost tests. Terraform plan tests validate configuration only;
the profile outputs are expectations, not measured performance or availability.
