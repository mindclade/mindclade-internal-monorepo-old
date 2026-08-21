# Service perimeter module

Creates one identity-scoped VPC Service Controls perimeter beneath the organization's existing
Access Context Manager policy. This release is fail-closed to explicit dry-run: enforcement requires
connected violation-log evidence and a later governed interface. Broad egress and IP-based access
levels are rejected.

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
| <a name="input_access_levels"></a> [access\_levels](#input\_access\_levels) | IP-based access levels are forbidden by the initial identity-only perimeter contract. | `map(object({ title = string }))` | `{}` | no |
| <a name="input_egress_policies"></a> [egress\_policies](#input\_egress\_policies) | Egress remains closed for the initial qualified perimeter release. | `list(object({ title = string }))` | `[]` | no |
| <a name="input_ingress_policies"></a> [ingress\_policies](#input\_ingress\_policies) | n/a | <pre>list(object({<br/>    title = string<br/>    from = object({<br/>      identities           = set(string)<br/>      identity_type        = optional(string)<br/>      source_access_levels = set(string)<br/>    })<br/>    to = object({<br/>      resources = set(string)<br/>      operations = map(object({<br/>        methods = set(string)<br/>      }))<br/>    })<br/>  }))</pre> | n/a | yes |
| <a name="input_org_id"></a> [org\_id](#input\_org\_id) | n/a | `string` | n/a | yes |
| <a name="input_perimeter"></a> [perimeter](#input\_perimeter) | n/a | <pre>object({<br/>    name                      = string<br/>    title                     = string<br/>    use_explicit_dry_run_spec = bool<br/>    resources                 = set(string)<br/>    restricted_services       = set(string)<br/>    vpc_accessible_services = object({<br/>      enable_restriction = bool<br/>      allowed_services   = set(string)<br/>    })<br/>  })</pre> | n/a | yes |
| <a name="input_policy_name"></a> [policy\_name](#input\_policy\_name) | n/a | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_dry_run"></a> [dry\_run](#output\_dry\_run) | n/a |
| <a name="output_perimeter_name"></a> [perimeter\_name](#output\_perimeter\_name) | n/a |
<!-- END_TF_DOCS -->
