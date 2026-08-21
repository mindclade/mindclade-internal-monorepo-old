# Project factory module

Creates a keyed project set with one aggregate budget, protected liens, optional Shared VPC
host/service relationships, and an optional metrics scope. It composes the single-project module so
project labels, API lifecycle, default-network denial, and deletion protection remain one contract.

The caller owns the hierarchy folder and declares every role explicitly. Service projects cannot be
created without exactly one host in the same state, and extra preview services apply only to the
metrics-scope host.

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
| <a name="input_billing_account"></a> [billing\_account](#input\_billing\_account) | Billing account ID attached to every project. | `string` | n/a | yes |
| <a name="input_budget_amount"></a> [budget\_amount](#input\_budget\_amount) | Optional aggregate monthly USD budget for this project set. | `number` | `null` | no |
| <a name="input_deletion_policy"></a> [deletion\_policy](#input\_deletion\_policy) | Fail-closed project lifecycle; only PREVENT is supported. | `string` | `"PREVENT"` | no |
| <a name="input_extra_services"></a> [extra\_services](#input\_extra\_services) | Additional APIs enabled only in the declared monitoring-scope host. | `set(string)` | `[]` | no |
| <a name="input_folder_id"></a> [folder\_id](#input\_folder\_id) | Parent folder resource name shared by the project set. | `string` | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | Labels merged with the child project module's mandatory ownership labels. | `map(string)` | `{}` | no |
| <a name="input_projects"></a> [projects](#input\_projects) | Projects keyed by a stable short name used by downstream state contracts. | <pre>map(object({<br/>    project_id                 = string<br/>    name                       = string<br/>    services                   = set(string)<br/>    lien                       = optional(bool, false)<br/>    shared_vpc_host            = optional(bool, false)<br/>    shared_vpc_service_project = optional(bool, false)<br/>    monitoring_scope_host      = optional(bool, false)<br/>  }))</pre> | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_monitoring_scope_project_id"></a> [monitoring\_scope\_project\_id](#output\_monitoring\_scope\_project\_id) | Metrics scope host project ID, or null when this set owns no scope. |
| <a name="output_project_ids"></a> [project\_ids](#output\_project\_ids) | Project IDs keyed by the caller's stable project key. |
| <a name="output_project_numbers"></a> [project\_numbers](#output\_project\_numbers) | Project numbers keyed by the caller's stable project key. |
| <a name="output_shared_vpc_host_project_id"></a> [shared\_vpc\_host\_project\_id](#output\_shared\_vpc\_host\_project\_id) | Shared VPC host project ID, or null when this set owns no host. |
<!-- END_TF_DOCS -->
