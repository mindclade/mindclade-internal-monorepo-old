# Artifact Registry Docker repository

This module creates one standard private Docker repository with immutable tags,
provider- and Terraform-level deletion guards, inherited vulnerability scanning,
and a bounded **untagged** cleanup candidate. Cleanup starts in dry-run mode and
cannot become destructive until the caller sets `cleanup_activation_approved = true`
in the same reviewed change.

Artifact Registry cannot delete an image that still has an immutable tag. Tagged
release artifacts are therefore intentionally outside this cleanup policy and may
grow without bound. The release governance owner must budget and approve their
retention or rotate whole repositories through a separate, evidence-preserving
workflow. Do not weaken tag immutability or silently describe total repository
retention as bounded.

The module deliberately does not enable APIs, create KMS keys, grant IAM, sign
images, create attestations, or configure admission. Callers must enable
`artifactregistry.googleapis.com`; enable `containerscanning.googleapis.com`
when automatic scanning is required; grant repository-scoped reader/writer
roles to separate workload identities; and verify the effective scanning-state
output. A CMEK, when supplied, must already grant the Artifact Registry service
agent encrypt/decrypt access and use the repository location.

```hcl
module "application_images" {
  source = "../../modules/artifact_registry"

  project_id    = "mindclade-production"
  location      = "us-central1"
  repository_id = "application-images"
  environment   = "production"
  owner         = "cloud-platform"

  untagged_retention_days = 45
  minimum_versions_to_keep = 25
}
```

Before activating cleanup, review dry-run logs for at least one complete cleanup
cycle, prove that rollback digests remain available, and record approval. Deploy
workloads by digest: immutable tags prevent moving or deleting a tag, but they do
not by themselves provide provenance, admission enforcement, or proof that
scanning is active. This module is an infrastructure contract, not deployed or
production-readiness evidence.

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
| <a name="input_cleanup_activation_approved"></a> [cleanup\_activation\_approved](#input\_cleanup\_activation\_approved) | Explicit acknowledgement required before cleanup\_policy\_dry\_run can be disabled | `bool` | `false` | no |
| <a name="input_cleanup_policy_dry_run"></a> [cleanup\_policy\_dry\_run](#input\_cleanup\_policy\_dry\_run) | Keep cleanup policies in log-only dry-run mode; defaults to the safe rollout state | `bool` | `true` | no |
| <a name="input_data_classification"></a> [data\_classification](#input\_data\_classification) | Repository data-classification governance label | `string` | `"internal"` | no |
| <a name="input_description"></a> [description](#input\_description) | Non-sensitive repository purpose shown in Artifact Registry | `string` | `"Managed private Docker artifacts."` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Deployment environment governance label | `string` | n/a | yes |
| <a name="input_kms_key_name"></a> [kms\_key\_name](#input\_kms\_key\_name) | Optional CMEK crypto-key resource name; the key location must match the repository | `string` | `null` | no |
| <a name="input_labels"></a> [labels](#input\_labels) | Additional repository labels; baseline governance labels take precedence | `map(string)` | `{}` | no |
| <a name="input_location"></a> [location](#input\_location) | Regional or multi-regional Artifact Registry location | `string` | n/a | yes |
| <a name="input_minimum_versions_to_keep"></a> [minimum\_versions\_to\_keep](#input\_minimum\_versions\_to\_keep) | Minimum most-recent versions retained for each package | `number` | `20` | no |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team governance label | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Google Cloud project that owns the repository | `string` | n/a | yes |
| <a name="input_repository_id"></a> [repository\_id](#input\_repository\_id) | Stable Docker repository ID within the selected project and location | `string` | n/a | yes |
| <a name="input_untagged_retention_days"></a> [untagged\_retention\_days](#input\_untagged\_retention\_days) | Age in days after which untagged versions become cleanup candidates | `number` | `30` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_cleanup_contract"></a> [cleanup\_contract](#output\_cleanup\_contract) | Reviewed cleanup-policy inputs for change-control and validation evidence |
| <a name="output_cleanup_policy_dry_run"></a> [cleanup\_policy\_dry\_run](#output\_cleanup\_policy\_dry\_run) | Whether cleanup policies are prevented from deleting artifacts |
| <a name="output_repository_id"></a> [repository\_id](#output\_repository\_id) | Artifact Registry repository ID |
| <a name="output_repository_name"></a> [repository\_name](#output\_repository\_name) | Fully qualified Artifact Registry repository resource name |
| <a name="output_repository_uri"></a> [repository\_uri](#output\_repository\_uri) | Docker repository URI; publish and deploy immutable digest references beneath this URI |
| <a name="output_vulnerability_scanning_state"></a> [vulnerability\_scanning\_state](#output\_vulnerability\_scanning\_state) | Provider-reported effective vulnerability-scanning state; verify this after deployment |
| <a name="output_vulnerability_scanning_state_reason"></a> [vulnerability\_scanning\_state\_reason](#output\_vulnerability\_scanning\_state\_reason) | Provider-reported reason for the effective vulnerability-scanning state |
<!-- END_TF_DOCS -->
