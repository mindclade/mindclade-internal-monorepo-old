# Regional GKE platform module

This module creates a regional Standard GKE cluster and a dedicated redundant
system node pool. It requires an existing VPC/subnet and secondary ranges and
enforces private nodes and control plane with an RFC1918 `/28`, Dataplane V2,
Workload
Identity Federation for GKE, Shielded Nodes, Binary Authorization policy
enforcement, Google Groups for RBAC, intranode visibility, managed release/maintenance,
control-plane and workload logging,
Managed Service for Prometheus, cost allocation, deletion protection, and managed
CSI/backup agents. Restricted-data clusters require application-layer Kubernetes
Secrets encryption with Cloud KMS.

The environment channel policy uses a development-only `RAPID`/`CANARY` cohort and
`REGULAR`/`QUALIFIED` for staging, production, and control clusters. This exposes an upgrade in
development without placing production on a different slower channel than staging. The current
source baseline sets `1.36.2-gke.2064000` as the minimum control-plane and initial system-node
version; connected planning must verify that baseline is still available in the selected region and
channel before any apply.
The Cloud Storage FUSE CSI driver defaults off: NOVA training uses
generation-bound object APIs and must not mount checkpoint storage through
GCS Fuse. Changing either value requires a new immutable platform lock and
upgrade, rollback, workload, and accelerator qualification evidence.

Dataplane V2 supplies Kubernetes NetworkPolicy enforcement itself. The module
deliberately omits the legacy `network_policy` API block because GKE rejects that
block when `ADVANCED_DATAPATH` is selected.

The monitoring contract includes managed DCGM metrics through Managed Service for
Prometheus. GPU qualification consumes those existing metrics and reads XID events
from the GKE GPU device-plugin logs; it does not deploy a second DCGM exporter.

The module does not create workload node pools, Kubernetes namespaces/RBAC/policy,
Backup for GKE plans, Binary Authorization attestations, firewall rules, NAT, DNS,
KMS IAM, service-account IAM, notification channels, or quotas. Those cross state
and privilege boundaries. The system-node service account must be user managed and
keyless; Pod access uses Kubernetes service accounts through Workload Identity.

The pin and Terraform plan tests are **Proposed deployment configuration**, not
observed cluster state. Before rollout, verify current GKE version/region support, IP capacity, private
control-plane reachability, maintenance exclusions, release compatibility, quotas,
Pod Security/NetworkPolicy admission, Binary Authorization deny canaries, log and
metric routing, alert paging, backup/restore, upgrade/rollback, zonal impairment,
and cost attribution. Offline tests do not create or exercise a cluster.

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
| <a name="input_channel_policy"></a> [channel\_policy](#input\_channel\_policy) | Upgrade cohort paired with release\_channel: CANARY is development-only RAPID; QUALIFIED is REGULAR | `string` | `"QUALIFIED"` | no |
| <a name="input_data_classification"></a> [data\_classification](#input\_data\_classification) | Data-classification label applied to cluster and node resources | `string` | `"internal"` | no |
| <a name="input_database_encryption_key_name"></a> [database\_encryption\_key\_name](#input\_database\_encryption\_key\_name) | Optional regional CryptoKey for application-layer Kubernetes Secrets encryption; required for restricted data | `string` | `null` | no |
| <a name="input_enable_backup_agent"></a> [enable\_backup\_agent](#input\_enable\_backup\_agent) | Enable the Backup for GKE agent; backup plans, IAM, retention, and restore tests remain separate resources | `bool` | `true` | no |
| <a name="input_enable_gcs_fuse_csi_driver"></a> [enable\_gcs\_fuse\_csi\_driver](#input\_enable\_gcs\_fuse\_csi\_driver) | Enable the managed Cloud Storage FUSE CSI driver; NOVA training requires this to remain false and uses generation-bound object APIs | `bool` | `false` | no |
| <a name="input_enable_secret_sync"></a> [enable\_secret\_sync](#input\_enable\_secret\_sync) | Enable the GKE Secret Manager SecretSync controller for workloads that require native Kubernetes Secret objects | `bool` | `false` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment label applied to cluster and node resources | `string` | n/a | yes |
| <a name="input_kubernetes_version"></a> [kubernetes\_version](#input\_kubernetes\_version) | Pinned minimum control-plane and initial system node version for the NOVA v1 training qualification tuple | `string` | `"1.36.2-gke.2064000"` | no |
| <a name="input_maintenance_window"></a> [maintenance\_window](#input\_maintenance\_window) | Recurring UTC maintenance window in RFC3339/RFC5545 form | <pre>object({<br/>    start_time = string<br/>    end_time   = string<br/>    recurrence = string<br/>  })</pre> | <pre>{<br/>  "end_time": "2025-01-05T10:00:00Z",<br/>  "recurrence": "FREQ=WEEKLY;BYDAY=SU",<br/>  "start_time": "2025-01-05T02:00:00Z"<br/>}</pre> | no |
| <a name="input_master_authorized_networks"></a> [master\_authorized\_networks](#input\_master\_authorized\_networks) | Non-public CIDRs permitted to reach the private control-plane endpoint | <pre>list(object({<br/>    cidr_block   = string<br/>    display_name = string<br/>  }))</pre> | n/a | yes |
| <a name="input_master_ipv4_cidr_block"></a> [master\_ipv4\_cidr\_block](#input\_master\_ipv4\_cidr\_block) | Dedicated non-overlapping RFC1918 /28 for the private GKE control plane | `string` | n/a | yes |
| <a name="input_name"></a> [name](#input\_name) | Regional GKE cluster name; limited to leave room for the managed system node-pool suffix | `string` | n/a | yes |
| <a name="input_network"></a> [network](#input\_network) | Existing VPC network name or self-link | `string` | n/a | yes |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team label applied to cluster and node resources | `string` | n/a | yes |
| <a name="input_pod_secondary_range_name"></a> [pod\_secondary\_range\_name](#input\_pod\_secondary\_range\_name) | Existing subnetwork secondary range used for Pod addresses | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | GCP project ID that owns the cluster | `string` | n/a | yes |
| <a name="input_rbac_security_group"></a> [rbac\_security\_group](#input\_rbac\_security\_group) | Google Group configured as the GKE security-groups parent for group-based Kubernetes RBAC | `string` | n/a | yes |
| <a name="input_region"></a> [region](#input\_region) | GCP region for the regional control plane, for example us-central1 | `string` | n/a | yes |
| <a name="input_release_channel"></a> [release\_channel](#input\_release\_channel) | GKE release channel: RAPID for the development canary or REGULAR for qualified staging, production, and control clusters | `string` | `"REGULAR"` | no |
| <a name="input_resource_labels"></a> [resource\_labels](#input\_resource\_labels) | Additional GCP resource labels; module governance labels take precedence | `map(string)` | `{}` | no |
| <a name="input_secret_sync_rotation_interval"></a> [secret\_sync\_rotation\_interval](#input\_secret\_sync\_rotation\_interval) | Rotation interval for GKE SecretSync; used only when enable\_secret\_sync is true | `string` | `"120s"` | no |
| <a name="input_service_secondary_range_name"></a> [service\_secondary\_range\_name](#input\_service\_secondary\_range\_name) | Existing subnetwork secondary range used for Service addresses | `string` | n/a | yes |
| <a name="input_subnetwork"></a> [subnetwork](#input\_subnetwork) | Existing regional subnetwork name or self-link | `string` | n/a | yes |
| <a name="input_system_node_pool_boot_disk_size_gb"></a> [system\_node\_pool\_boot\_disk\_size\_gb](#input\_system\_node\_pool\_boot\_disk\_size\_gb) | Boot-disk size for system nodes | `number` | `100` | no |
| <a name="input_system_node_pool_machine_type"></a> [system\_node\_pool\_machine\_type](#input\_system\_node\_pool\_machine\_type) | Machine type for the non-accelerator system node pool | `string` | `"e2-standard-4"` | no |
| <a name="input_system_node_pool_max_pods_per_node"></a> [system\_node\_pool\_max\_pods\_per\_node](#input\_system\_node\_pool\_max\_pods\_per\_node) | Maximum Pods per system node | `number` | `64` | no |
| <a name="input_system_node_pool_total_max_nodes"></a> [system\_node\_pool\_total\_max\_nodes](#input\_system\_node\_pool\_total\_max\_nodes) | Maximum total system nodes across the regional node pool | `number` | `9` | no |
| <a name="input_system_node_pool_total_min_nodes"></a> [system\_node\_pool\_total\_min\_nodes](#input\_system\_node\_pool\_total\_min\_nodes) | Minimum total system nodes across the regional node pool | `number` | `3` | no |
| <a name="input_system_node_service_account_email"></a> [system\_node\_service\_account\_email](#input\_system\_node\_service\_account\_email) | Dedicated user-managed service account for system node VMs; grant its permissions outside this module | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_cluster_endpoint"></a> [cluster\_endpoint](#output\_cluster\_endpoint) | Private Kubernetes API endpoint; treat as sensitive network metadata |
| <a name="output_cluster_id"></a> [cluster\_id](#output\_cluster\_id) | Fully qualified GKE cluster identifier |
| <a name="output_cluster_location"></a> [cluster\_location](#output\_cluster\_location) | GKE regional control-plane location |
| <a name="output_cluster_name"></a> [cluster\_name](#output\_cluster\_name) | GKE cluster name |
| <a name="output_network"></a> [network](#output\_network) | VPC network attached to the cluster |
| <a name="output_project_id"></a> [project\_id](#output\_project\_id) | Project containing the GKE cluster |
| <a name="output_subnetwork"></a> [subnetwork](#output\_subnetwork) | VPC subnetwork attached to the cluster |
| <a name="output_system_node_pool_name"></a> [system\_node\_pool\_name](#output\_system\_node\_pool\_name) | Managed non-accelerator system node-pool name |
| <a name="output_workload_identity_pool"></a> [workload\_identity\_pool](#output\_workload\_identity\_pool) | Workload Identity Federation pool configured for Kubernetes service accounts |
<!-- END_TF_DOCS -->
