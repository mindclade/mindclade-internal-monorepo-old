# Bazel remote execution foundation

This module creates the Google Cloud foundation for a private Bazel remote execution
service on an existing regional GKE Standard cluster:

- a protected, autoscaled general-purpose or high-memory CPU node pool through the
  canonical `cpu_node_pool` module;
- a dedicated protected node identity with the narrow GKE node role;
- a separate protected executor workload identity, bound keylessly to one Kubernetes
  service account;
- additive Artifact Registry/project IAM and append-only read/create access to an
  existing `bazel_remote_cache` bucket;
- an immutable executor image contract and the node selector/toleration values GitOps
  must deploy.

```hcl
module "bazel_remote_execution" {
  source = "../../modules/bazel_remote_execution"

  project_id               = "mindclade-build"
  cluster_name             = "mindclade-platform"
  region                   = "us-central1"
  node_locations           = ["us-central1-a", "us-central1-b"]
  pod_secondary_range_name = "gke-pods"
  node_service_account_id  = "bazel-executor-nodes"
  executor_image           = "us-central1-docker.pkg.dev/mindclade-build/workers/bazel@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  cache_bucket_name        = "mindclade-bazel-cas"
  environment              = "production"
  owner                    = "release-engineering"
}
```

## Boundary

Terraform does **not** deploy the remote-execution server, Kubernetes namespace,
KSA, NetworkPolicy, PodDisruptionBudget, autoscaler driven by execution backlog,
certificates, load balancer, Bazel platform, Nix worker closure, or endpoint. Those
belong to GitOps, Bazel, and Nix. `gitops_contract` is the exact handoff; a green
Terraform plan is not a claim that remote execution works.

The remote cache bucket is created by `bazel_remote_cache`, never here. The executor
can read and create objects but cannot administer the bucket or its lifecycle. CAS,
Nix binary cache, platform artifacts, and audit evidence stay separate because their
mutation, retention, and recovery policies differ.

SPOT capacity is opt-in, requires the exact acknowledgement, and must scale to zero.
Callers must prove interruption retry, deterministic outputs, bounded cancellation,
cache miss/corruption behavior, multi-zone capacity, private connectivity, image
attestation, and cold/warm performance before enabling it. No service-account key is
created or accepted.

## Upgrade and rollback

Roll out a new immutable executor image through GitOps, canary remote execution, then
change the default platform. Keep the prior digest and node pool until result parity,
cache compatibility, cancellation, and performance pass. Rollback selects the prior
digest or local execution; do not delete protected identities or the cache during an
incident. Node-pool replacement is a reviewed capacity migration, not an ordinary
in-place edit.

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
| <a name="input_additional_taints"></a> [additional\_taints](#input\_additional\_taints) | Additional dedicated-pool taints | <pre>list(object({<br/>    key    = string<br/>    value  = string<br/>    effect = string<br/>  }))</pre> | `[]` | no |
| <a name="input_boot_disk_kms_key"></a> [boot\_disk\_kms\_key](#input\_boot\_disk\_kms\_key) | Optional regional CMEK CryptoKey for worker boot disks | `string` | `null` | no |
| <a name="input_boot_disk_size_gb"></a> [boot\_disk\_size\_gb](#input\_boot\_disk\_size\_gb) | Worker boot disk size in GiB | `number` | `200` | no |
| <a name="input_boot_disk_type"></a> [boot\_disk\_type](#input\_boot\_disk\_type) | Worker boot disk type | `string` | `"pd-balanced"` | no |
| <a name="input_cache_bucket_name"></a> [cache\_bucket\_name](#input\_cache\_bucket\_name) | Existing bucket created by bazel\_remote\_cache | `string` | n/a | yes |
| <a name="input_capacity_type"></a> [capacity\_type](#input\_capacity\_type) | ON\_DEMAND or explicitly acknowledged SPOT worker capacity | `string` | `"ON_DEMAND"` | no |
| <a name="input_cluster_name"></a> [cluster\_name](#input\_cluster\_name) | Existing regional GKE Standard cluster name | `string` | n/a | yes |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment governance label | `string` | n/a | yes |
| <a name="input_executor_image"></a> [executor\_image](#input\_executor\_image) | Immutable executor image reference deployed by GitOps; tags are forbidden | `string` | n/a | yes |
| <a name="input_executor_project_roles"></a> [executor\_project\_roles](#input\_executor\_project\_roles) | Additional additive project roles for the executor workload; administrative and basic roles are forbidden | `set(string)` | <pre>[<br/>  "roles/artifactregistry.reader"<br/>]</pre> | no |
| <a name="input_executor_service_account_id"></a> [executor\_service\_account\_id](#input\_executor\_service\_account\_id) | Account ID for the keyless Bazel executor workload service account | `string` | `"bazel-remote-executor"` | no |
| <a name="input_kubernetes_namespace"></a> [kubernetes\_namespace](#input\_kubernetes\_namespace) | Namespace of the executor Kubernetes service account deployed by GitOps | `string` | `"build"` | no |
| <a name="input_kubernetes_service_account"></a> [kubernetes\_service\_account](#input\_kubernetes\_service\_account) | Executor Kubernetes service account deployed by GitOps | `string` | `"bazel-remote-executor"` | no |
| <a name="input_machine_type"></a> [machine\_type](#input\_machine\_type) | Optional reviewed machine-type override | `string` | `null` | no |
| <a name="input_max_pods_per_node"></a> [max\_pods\_per\_node](#input\_max\_pods\_per\_node) | Maximum pods per worker node | `number` | `32` | no |
| <a name="input_node_drain_grace_period"></a> [node\_drain\_grace\_period](#input\_node\_drain\_grace\_period) | Grace period for node-pool deletion drains | `string` | `"600s"` | no |
| <a name="input_node_drain_pdb_timeout"></a> [node\_drain\_pdb\_timeout](#input\_node\_drain\_pdb\_timeout) | PodDisruptionBudget timeout for node-pool deletion drains | `string` | `"600s"` | no |
| <a name="input_node_labels"></a> [node\_labels](#input\_node\_labels) | Additional Kubernetes node labels; the workload label is module-owned | `map(string)` | `{}` | no |
| <a name="input_node_locations"></a> [node\_locations](#input\_node\_locations) | Two or more zones in region used by the worker pool | `set(string)` | n/a | yes |
| <a name="input_node_pool_name"></a> [node\_pool\_name](#input\_node\_pool\_name) | Dedicated Bazel remote execution node-pool name | `string` | `"bazel-executors"` | no |
| <a name="input_node_service_account_id"></a> [node\_service\_account\_id](#input\_node\_service\_account\_id) | Account ID for the dedicated node VM service account created by cpu\_node\_pool | `string` | n/a | yes |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team governance label | `string` | n/a | yes |
| <a name="input_pod_secondary_range_name"></a> [pod\_secondary\_range\_name](#input\_pod\_secondary\_range\_name) | Existing pod secondary range used by the cluster | `string` | n/a | yes |
| <a name="input_profile"></a> [profile](#input\_profile) | GENERAL\_PURPOSE or HIGH\_MEMORY CPU worker profile | `string` | `"GENERAL_PURPOSE"` | no |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project containing the GKE cluster, worker pool, and executor identity | `string` | n/a | yes |
| <a name="input_region"></a> [region](#input\_region) | Region of the existing cluster | `string` | n/a | yes |
| <a name="input_resource_labels"></a> [resource\_labels](#input\_resource\_labels) | Additional GCP labels; module governance labels take precedence | `map(string)` | `{}` | no |
| <a name="input_spot_approval"></a> [spot\_approval](#input\_spot\_approval) | Exact acknowledgement required for interruption-prone remote workers: I ACCEPT EVICTION AND CAPACITY-LOSS RISK | `string` | `null` | no |
| <a name="input_total_max_nodes"></a> [total\_max\_nodes](#input\_total\_max\_nodes) | Maximum worker nodes across all node locations | `number` | `20` | no |
| <a name="input_total_min_nodes"></a> [total\_min\_nodes](#input\_total\_min\_nodes) | Minimum worker nodes across all node locations | `number` | `1` | no |
| <a name="input_upgrade_max_surge"></a> [upgrade\_max\_surge](#input\_upgrade\_max\_surge) | Maximum surge nodes during upgrades | `number` | `1` | no |
| <a name="input_upgrade_max_unavailable"></a> [upgrade\_max\_unavailable](#input\_upgrade\_max\_unavailable) | Maximum unavailable nodes during upgrades | `number` | `0` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_executor_service_account"></a> [executor\_service\_account](#output\_executor\_service\_account) | Keyless executor workload identity |
| <a name="output_gitops_contract"></a> [gitops\_contract](#output\_gitops\_contract) | Values the Kubernetes/GitOps owner must use when deploying the remote execution service |
| <a name="output_node_service_account"></a> [node\_service\_account](#output\_node\_service\_account) | Dedicated node VM identity |
| <a name="output_project_iam_grants"></a> [project\_iam\_grants](#output\_project\_iam\_grants) | Additive node and executor project grants created by this composition |
| <a name="output_qualification_requirements"></a> [qualification\_requirements](#output\_qualification\_requirements) | Runtime evidence still required; Terraform cannot prove these conditions |
| <a name="output_required_apis"></a> [required\_apis](#output\_required\_apis) | APIs the project factory must enable before this module is applied |
| <a name="output_worker_pool"></a> [worker\_pool](#output\_worker\_pool) | Protected CPU worker pool contract |
<!-- END_TF_DOCS -->
