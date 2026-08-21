# GKE accelerator node-pool module

This module adds one single-zone accelerator pool to an existing regional GKE
cluster. The admitted profiles are deliberately narrow: A3 Mega H100, A3 Ultra
H200, and A4 B200 eight-GPU nodes used by the NOVA execution-plan contract. Each profile fixes
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
Ultra H200 and A4 B200, which must use Spot, Flex Start, queued provisioning, or an approved
reservation. Queued provisioning enables both provider-required queued and Flex Start
settings and has a seven-day maximum lifetime. Standalone Flex Start is preview-only
and requires an explicit approval input. Both modes must scale from zero and disable
MIG compact placement; reservations require an exact reservation name. Ordinary
compact pools default to unavailable-first upgrades to avoid assuming spare accelerator
capacity. Flex Start and queued provisioning explicitly opt out of reservations.
Boot-disk CMEK selects `pd-ssd` for H100; it is rejected for H200 and B200 because those
profiles require Hyperdisk and GKE does not currently support boot-disk CMEK on Hyperdisk.
Capacity, quota, zone availability, Dynamic Workload
Scheduler, Compact Placement, driver compatibility, GPUDirect/RDMA components,
Kueue/JobSet policy, workload checkpointing, and artifact durability remain
environment and Kubernetes-layer responsibilities.

This module is the accelerator-capacity authority selected by the
`infrastructure-live` estate. The Kubernetes layer owns only the matching node
labels, taint, Kueue ResourceFlavors, topology, queues, and held quota; it does not
create `ComputeClass` resources. An environment must never add a second capacity
authority for the same GPU profile and zone. Moving to `ComputeClass` would require
a reviewed two-phase migration that first holds Kueue and removes these fixed pools
from desired state without orphaning their Terraform state.

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
| <a name="input_additional_taints"></a> [additional\_taints](#input\_additional\_taints) | Additional Kubernetes taints; nvidia.com/gpu is managed by this module | <pre>list(object({<br/>    key    = string<br/>    value  = string<br/>    effect = string<br/>  }))</pre> | `[]` | no |
| <a name="input_boot_disk_kms_key"></a> [boot\_disk\_kms\_key](#input\_boot\_disk\_kms\_key) | Optional regional Cloud KMS CryptoKey for H100 pd-ssd boot disks; unsupported for the H200 and B200 Hyperdisk profiles | `string` | `null` | no |
| <a name="input_boot_disk_size_gb"></a> [boot\_disk\_size\_gb](#input\_boot\_disk\_size\_gb) | Boot-disk size for each accelerator node | `number` | `250` | no |
| <a name="input_capacity_mode"></a> [capacity\_mode](#input\_capacity\_mode) | Capacity acquisition mode; quota, reservation, and Dynamic Workload Scheduler policy remain environment responsibilities | `string` | `"ON_DEMAND"` | no |
| <a name="input_cluster_name"></a> [cluster\_name](#input\_cluster\_name) | Existing regional GKE cluster name | `string` | n/a | yes |
| <a name="input_data_classification"></a> [data\_classification](#input\_data\_classification) | Data-classification label applied to node resources | `string` | `"internal"` | no |
| <a name="input_enable_compact_placement"></a> [enable\_compact\_placement](#input\_enable\_compact\_placement) | Use a compact placement policy for lower-latency multi-node collectives | `bool` | `true` | no |
| <a name="input_enable_preview_flex_start"></a> [enable\_preview\_flex\_start](#input\_enable\_preview\_flex\_start) | Explicitly approve the standalone FLEX\_START preview after environment qualification; ignored for the GA queued-provisioning combination | `bool` | `false` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment label applied to node resources | `string` | n/a | yes |
| <a name="input_gpu_driver_version"></a> [gpu\_driver\_version](#input\_gpu\_driver\_version) | GKE-managed NVIDIA driver channel | `string` | `"DEFAULT"` | no |
| <a name="input_max_pods_per_node"></a> [max\_pods\_per\_node](#input\_max\_pods\_per\_node) | Maximum Pods per accelerator node | `number` | `16` | no |
| <a name="input_max_run_duration"></a> [max\_run\_duration](#input\_max\_run\_duration) | Maximum Flex Start or queued-provisioning node lifetime, bounded to seven days | `string` | `"86400s"` | no |
| <a name="input_name"></a> [name](#input\_name) | GPU node-pool name | `string` | n/a | yes |
| <a name="input_node_drain_grace_period"></a> [node\_drain\_grace\_period](#input\_node\_drain\_grace\_period) | Grace period used when draining an accelerator node | `string` | `"3600s"` | no |
| <a name="input_node_drain_pdb_timeout"></a> [node\_drain\_pdb\_timeout](#input\_node\_drain\_pdb\_timeout) | Maximum time to honor PodDisruptionBudgets during an accelerator-node drain | `string` | `"3600s"` | no |
| <a name="input_node_labels"></a> [node\_labels](#input\_node\_labels) | Additional Kubernetes node labels; module identity labels take precedence | `map(string)` | `{}` | no |
| <a name="input_node_service_account_email"></a> [node\_service\_account\_email](#input\_node\_service\_account\_email) | Dedicated user-managed service account for GPU node VMs; grant permissions outside this module | `string` | n/a | yes |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team label applied to node resources | `string` | n/a | yes |
| <a name="input_pod_secondary_range_name"></a> [pod\_secondary\_range\_name](#input\_pod\_secondary\_range\_name) | Existing cluster Pod secondary-range name | `string` | n/a | yes |
| <a name="input_profile"></a> [profile](#input\_profile) | Fixed accelerator profile admitted by the NOVA execution-plan v2 contract | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | GCP project ID that owns the GKE cluster | `string` | n/a | yes |
| <a name="input_region"></a> [region](#input\_region) | Regional GKE control-plane location | `string` | n/a | yes |
| <a name="input_reservation_name"></a> [reservation\_name](#input\_reservation\_name) | Specific same-region Compute Engine reservation consumed in RESERVATION mode | `string` | `null` | no |
| <a name="input_resource_labels"></a> [resource\_labels](#input\_resource\_labels) | Additional GCP resource labels; module governance labels take precedence | `map(string)` | `{}` | no |
| <a name="input_total_max_nodes"></a> [total\_max\_nodes](#input\_total\_max\_nodes) | Maximum total nodes; defaults to one two-node NOVA training slice | `number` | `2` | no |
| <a name="input_total_min_nodes"></a> [total\_min\_nodes](#input\_total\_min\_nodes) | Minimum total nodes in this single-zone pool | `number` | `0` | no |
| <a name="input_upgrade_max_surge"></a> [upgrade\_max\_surge](#input\_upgrade\_max\_surge) | Extra accelerator nodes allowed during a surge upgrade; requires quota/capacity | `number` | `0` | no |
| <a name="input_upgrade_max_unavailable"></a> [upgrade\_max\_unavailable](#input\_upgrade\_max\_unavailable) | Accelerator nodes allowed to be unavailable during an upgrade | `number` | `1` | no |
| <a name="input_zone"></a> [zone](#input\_zone) | Single approved GPU zone; automated accelerator networking does not support multi-zone node pools | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_accelerator_count_per_node"></a> [accelerator\_count\_per\_node](#output\_accelerator\_count\_per\_node) | Number of accelerators attached to each node |
| <a name="output_accelerator_type"></a> [accelerator\_type](#output\_accelerator\_type) | GKE accelerator type |
| <a name="output_capacity_mode"></a> [capacity\_mode](#output\_capacity\_mode) | Configured accelerator capacity acquisition mode |
| <a name="output_fabric"></a> [fabric](#output\_fabric) | Expected accelerator fabric; live qualification remains required |
| <a name="output_machine_type"></a> [machine\_type](#output\_machine\_type) | Compute Engine accelerator-optimized machine type |
| <a name="output_managed_instance_group_urls"></a> [managed\_instance\_group\_urls](#output\_managed\_instance\_group\_urls) | Managed instance groups backing the node pool |
| <a name="output_node_pool_id"></a> [node\_pool\_id](#output\_node\_pool\_id) | Fully qualified GKE node-pool identifier |
| <a name="output_node_pool_name"></a> [node\_pool\_name](#output\_node\_pool\_name) | GKE GPU node-pool name |
| <a name="output_profile"></a> [profile](#output\_profile) | NOVA accelerator profile configured by this node pool |
| <a name="output_zone"></a> [zone](#output\_zone) | Single zone configured for the accelerator node pool |
<!-- END_TF_DOCS -->
