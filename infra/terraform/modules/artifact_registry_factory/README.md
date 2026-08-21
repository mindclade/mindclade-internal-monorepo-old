# Artifact Registry factory module

Creates a typed, CMEK-protected collection of Docker and language-package repositories while
preserving one Terragrunt state boundary. Docker repositories can require immutable tags and
inherited vulnerability scanning. Cleanup policies always begin in dry-run mode.

The module deliberately reports, but does not apply, the cross-state KMS grant required by the
Artifact Registry service agent.

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
| <a name="input_enable_vulnerability_scanning"></a> [enable\_vulnerability\_scanning](#input\_enable\_vulnerability\_scanning) | Require inherited Artifact Analysis scanning for Docker repositories. | `bool` | `true` | no |
| <a name="input_encryption_key"></a> [encryption\_key](#input\_encryption\_key) | CMEK used by every repository; its owning state must grant the Artifact Registry service agent. | `string` | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | Governance labels applied to every repository. | `map(string)` | `{}` | no |
| <a name="input_location"></a> [location](#input\_location) | Artifact Registry location shared by the collection. | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project that owns the repository collection. | `string` | n/a | yes |
| <a name="input_repositories"></a> [repositories](#input\_repositories) | Repositories keyed by stable Terraform identity. | <pre>map(object({<br/>    format      = string<br/>    description = string<br/>    docker_config = optional(object({<br/>      immutable_tags = optional(bool, true)<br/>    }))<br/>    cleanup_policies = optional(map(object({<br/>      action               = string<br/>      condition_state      = optional(string)<br/>      older_than           = optional(string)<br/>      most_recent_versions = optional(number)<br/>    })), {})<br/>  }))</pre> | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_repositories"></a> [repositories](#output\_repositories) | Repository names and immutable publication roots keyed by caller identity. |
| <a name="output_required_kms_grant"></a> [required\_kms\_grant](#output\_required\_kms\_grant) | Exact cross-state grant the KMS owner must apply after resolving the project number. |
<!-- END_TF_DOCS -->
