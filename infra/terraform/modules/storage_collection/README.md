# Storage collection module

Owns a typed collection of CMEK-protected buckets in one existing Terragrunt state boundary.
Bucket Lock requires an explicit irreversible-action acknowledgement. Optional IAM deny policy
rules are accepted only for a one-bucket collection and receive an automatic resource-name
condition so a project-level policy cannot deny access to sibling buckets.

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
| <a name="input_buckets"></a> [buckets](#input\_buckets) | Buckets keyed by stable Terraform identity. | <pre>map(object({<br/>    name                          = string<br/>    location                      = optional(string)<br/>    hierarchical_namespace        = optional(bool, false)<br/>    versioning                    = optional(bool)<br/>    soft_delete_retention_seconds = optional(number)<br/>    retention_days                = optional(number)<br/>    lifecycle_rules = optional(list(object({<br/>      condition = object({<br/>        age                        = optional(number)<br/>        days_since_noncurrent_time = optional(number)<br/>        matches_storage_class      = optional(list(string))<br/>        num_newer_versions         = optional(number)<br/>        with_state                 = optional(string)<br/>      })<br/>      action = object({<br/>        type          = string<br/>        storage_class = optional(string)<br/>      })<br/>    })))<br/>  }))</pre> | n/a | yes |
| <a name="input_deny_policies"></a> [deny\_policies](#input\_deny\_policies) | Project-attached deny policies that this module automatically scopes to its sole managed bucket. | <pre>map(object({<br/>    display_name = string<br/>    rules = list(object({<br/>      denied_principals    = set(string)<br/>      denied_permissions   = set(string)<br/>      exception_principals = optional(set(string), [])<br/>    }))<br/>  }))</pre> | `{}` | no |
| <a name="input_encryption_key"></a> [encryption\_key](#input\_encryption\_key) | n/a | `string` | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | n/a | `map(string)` | `{}` | no |
| <a name="input_lifecycle_rules"></a> [lifecycle\_rules](#input\_lifecycle\_rules) | Collection-wide default lifecycle rules. | <pre>list(object({<br/>    condition = object({<br/>      age                        = optional(number)<br/>      days_since_noncurrent_time = optional(number)<br/>      matches_storage_class      = optional(list(string))<br/>      num_newer_versions         = optional(number)<br/>      with_state                 = optional(string)<br/>    })<br/>    action = object({<br/>      type          = string<br/>      storage_class = optional(string)<br/>    })<br/>  }))</pre> | `[]` | no |
| <a name="input_location"></a> [location](#input\_location) | n/a | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | n/a | `string` | n/a | yes |
| <a name="input_public_access_prevention"></a> [public\_access\_prevention](#input\_public\_access\_prevention) | n/a | `string` | `"enforced"` | no |
| <a name="input_retention_lock_confirmation"></a> [retention\_lock\_confirmation](#input\_retention\_lock\_confirmation) | Exact acknowledgement required when any bucket declares retention\_days. | `string` | `null` | no |
| <a name="input_soft_delete_retention_seconds"></a> [soft\_delete\_retention\_seconds](#input\_soft\_delete\_retention\_seconds) | n/a | `number` | `604800` | no |
| <a name="input_uniform_bucket_level_access"></a> [uniform\_bucket\_level\_access](#input\_uniform\_bucket\_level\_access) | n/a | `bool` | `true` | no |
| <a name="input_versioning"></a> [versioning](#input\_versioning) | n/a | `bool` | `true` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_buckets"></a> [buckets](#output\_buckets) | n/a |
| <a name="output_deny_policy_names"></a> [deny\_policy\_names](#output\_deny\_policy\_names) | n/a |
| <a name="output_required_kms_grant"></a> [required\_kms\_grant](#output\_required\_kms\_grant) | n/a |
<!-- END_TF_DOCS -->
