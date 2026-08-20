# GKE CPU node-pool module

This module creates one protected CPU node pool in an existing **regional GKE
Standard** cluster. It supports reviewed `GENERAL_PURPOSE` and `HIGH_MEMORY`
profiles, creates a dedicated user-managed node service account, and grants that
identity the additive `roles/container.defaultNodeServiceAccount` project role.
The default machines are `n2-standard-8` and `n2-highmem-8`; an explicit override
must remain consistent with the selected profile.

Nodes are private, use `COS_CONTAINERD`, GKE metadata, the `cloud-platform` OAuth
scope, Shielded VM secure boot and integrity monitoring, cgroup v2, a disabled
legacy metadata endpoint and read-only kubelet port, and a bounded per-Pod PID
limit. Autoscaling limits apply across all selected zones. Repair, automatic
upgrade, surge/unavailable limits, Pod-disruption-budget-aware draining, and both
Terraform and provider deletion guards are mandatory.

`HIGH_MEMORY` nodes receive
`scheduling.mindclade.dev/high-memory=true:NoSchedule`. Spot nodes receive
`scheduling.mindclade.dev/spot=true:NoSchedule`, must scale from zero, use the
autoscaler's `ANY` location policy, and require the exact acknowledgement
`I ACCEPT EVICTION AND CAPACITY-LOSS RISK`. On-demand pools must leave
`spot_approval` null. Module-owned labels and taints cannot be replaced by caller
values, and duplicate taint keys are rejected.

## Operating contract

The caller must provide the existing cluster name, regional location, one to three
zones in that region, and the cluster Pod secondary-range name. Private Google
Access, Workload Identity Federation for GKE, and control-plane/network policy
belong to the cluster and VPC modules. `GKE_METADATA` is enforced here and assumes
the existing cluster has Workload Identity Federation enabled; qualify Kubernetes
service-account to Google service-account bindings before scheduling workloads.
Grant Artifact Registry Reader in each image project and Service Usage Consumer
where cross-project image pulls require it; those grants are deliberately outside
this node-pool boundary. If `boot_disk_kms_key` is set, grant the GKE/Compute
service identities the required KMS permissions before creating the pool.

`prevent_destroy` and `deletion_policy = "PREVENT"` mean ordinary Terraform
destroy is intentionally blocked. A reviewed decommission must first change those
guards in code and drain workloads. Do not use Spot as the only reliable capacity
for a service. Confirm regional quota, machine availability, Pod disruption
budgets, upgrade headroom, logging/monitoring ingestion, image access, and CMEK
permissions with a saved plan and a staged rollout.

Provider-mock tests validate configuration and rejection paths only. They do not
prove quota, capacity, networking, successful drains, workload performance, or
cloud-side IAM propagation.

## Key outputs

- `node_pool`: resource identity and managed instance-group URLs.
- `node_service_account`: the dedicated keyless VM identity.
- `machine_type`, `profile`, and `capacity_type`: effective scheduling contract.
- `node_labels` and `node_taints`: effective Kubernetes placement metadata.
- `required_node_service_account_project_roles`: additive role granted here.
