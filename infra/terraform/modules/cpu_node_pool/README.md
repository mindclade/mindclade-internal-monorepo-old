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

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.9.0, < 2.0.0 |
| <a name="requirement_google"></a> [google](#requirement\_google) | >= 7.41.0, < 8.0.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_google"></a> [google](#provider\_google) | >= 7.41.0, < 8.0.0 |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_additional_taints"></a> [additional\_taints](#input\_additional\_taints) | Additional Kubernetes taints; high-memory and Spot isolation keys are module-managed | <pre>list(object({<br/>    key    = string<br/>    value  = string<br/>    effect = string<br/>  }))</pre> | `[]` | no |
| <a name="input_boot_disk_kms_key"></a> [boot\_disk\_kms\_key](#input\_boot\_disk\_kms\_key) | Optional regional Cloud KMS CryptoKey used for node boot disks | `string` | `null` | no |
| <a name="input_boot_disk_size_gb"></a> [boot\_disk\_size\_gb](#input\_boot\_disk\_size\_gb) | Boot-disk size for each node | `number` | `100` | no |
| <a name="input_boot_disk_type"></a> [boot\_disk\_type](#input\_boot\_disk\_type) | Boot-disk type for node VMs | `string` | `"pd-balanced"` | no |
| <a name="input_capacity_type"></a> [capacity\_type](#input\_capacity\_type) | Node capacity type; Spot is interruptible and requires an explicit acknowledgement | `string` | `"ON_DEMAND"` | no |
| <a name="input_cluster_name"></a> [cluster\_name](#input\_cluster\_name) | Existing regional GKE Standard cluster name | `string` | n/a | yes |
| <a name="input_data_classification"></a> [data\_classification](#input\_data\_classification) | Data-classification governance label | `string` | `"internal"` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment governance label | `string` | n/a | yes |
| <a name="input_machine_type"></a> [machine\_type](#input\_machine\_type) | Optional Compute Engine machine-type override; profile defaults are n2-standard-8 and n2-highmem-8 | `string` | `null` | no |
| <a name="input_max_pods_per_node"></a> [max\_pods\_per\_node](#input\_max\_pods\_per\_node) | Maximum Pods scheduled per node | `number` | `64` | no |
| <a name="input_name"></a> [name](#input\_name) | GKE node-pool name | `string` | n/a | yes |
| <a name="input_node_drain_grace_period"></a> [node\_drain\_grace\_period](#input\_node\_drain\_grace\_period) | Maximum node drain grace period | `string` | `"900s"` | no |
| <a name="input_node_drain_pdb_timeout"></a> [node\_drain\_pdb\_timeout](#input\_node\_drain\_pdb\_timeout) | Maximum time GKE waits for Pod disruption budgets during drain | `string` | `"600s"` | no |
| <a name="input_node_labels"></a> [node\_labels](#input\_node\_labels) | Additional Kubernetes node labels; module identity labels take precedence | `map(string)` | `{}` | no |
| <a name="input_node_locations"></a> [node\_locations](#input\_node\_locations) | One or more zones in region used by this pool | `set(string)` | n/a | yes |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team governance label | `string` | n/a | yes |
| <a name="input_pod_pids_limit"></a> [pod\_pids\_limit](#input\_pod\_pids\_limit) | Per-Pod process ID limit enforced by kubelet | `number` | `4096` | no |
| <a name="input_pod_secondary_range_name"></a> [pod\_secondary\_range\_name](#input\_pod\_secondary\_range\_name) | Existing cluster Pod secondary-range name | `string` | n/a | yes |
| <a name="input_profile"></a> [profile](#input\_profile) | Reviewed workload profile controlling the default machine type and isolation taints | `string` | `"GENERAL_PURPOSE"` | no |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | GCP project ID that owns the existing GKE cluster and dedicated node service account | `string` | n/a | yes |
| <a name="input_region"></a> [region](#input\_region) | Regional GKE control-plane location | `string` | n/a | yes |
| <a name="input_resource_labels"></a> [resource\_labels](#input\_resource\_labels) | Additional GCP resource labels; module governance labels take precedence | `map(string)` | `{}` | no |
| <a name="input_service_account_display_name"></a> [service\_account\_display\_name](#input\_service\_account\_display\_name) | Optional human-readable display name for the dedicated node service account | `string` | `null` | no |
| <a name="input_service_account_id"></a> [service\_account\_id](#input\_service\_account\_id) | Account ID for the dedicated user-managed node VM service account created by this module | `string` | n/a | yes |
| <a name="input_spot_approval"></a> [spot\_approval](#input\_spot\_approval) | Exact acknowledgement required for interruptible Spot nodes | `string` | `null` | no |
| <a name="input_total_max_nodes"></a> [total\_max\_nodes](#input\_total\_max\_nodes) | Maximum total nodes across all node\_locations | `number` | `10` | no |
| <a name="input_total_min_nodes"></a> [total\_min\_nodes](#input\_total\_min\_nodes) | Minimum total nodes across all node\_locations | `number` | `1` | no |
| <a name="input_upgrade_max_surge"></a> [upgrade\_max\_surge](#input\_upgrade\_max\_surge) | Additional nodes permitted during a surge upgrade | `number` | `1` | no |
| <a name="input_upgrade_max_unavailable"></a> [upgrade\_max\_unavailable](#input\_upgrade\_max\_unavailable) | Nodes allowed to be unavailable during an upgrade | `number` | `0` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_capacity_type"></a> [capacity\_type](#output\_capacity\_type) | Configured ON\_DEMAND or SPOT capacity type |
| <a name="output_machine_type"></a> [machine\_type](#output\_machine\_type) | Effective Compute Engine machine type |
| <a name="output_node_labels"></a> [node\_labels](#output\_node\_labels) | Effective Kubernetes node labels, including module-owned identity labels |
| <a name="output_node_pool"></a> [node\_pool](#output\_node\_pool) | Created GKE CPU node-pool identity and backing managed instance groups |
| <a name="output_node_service_account"></a> [node\_service\_account](#output\_node\_service\_account) | Dedicated keyless VM service account created for the node pool |
| <a name="output_node_taints"></a> [node\_taints](#output\_node\_taints) | Effective Kubernetes taints, including module-owned high-memory and Spot isolation |
| <a name="output_profile"></a> [profile](#output\_profile) | Configured workload profile |
| <a name="output_required_node_service_account_project_roles"></a> [required\_node\_service\_account\_project\_roles](#output\_required\_node\_service\_account\_project\_roles) | Additive project roles this module grants to the dedicated node service account; image-project Artifact Registry access remains separately scoped |
<!-- END_TF_DOCS -->
