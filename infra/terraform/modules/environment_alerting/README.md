# Environment alerting module

Creates actionable notification channels and threshold alert policies without taking ownership of
metrics-scope memberships. `project_factory` owns membership. SLO resources are intentionally absent
until service owners provide explicit good and total metric filters.

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
| <a name="input_alert_policies"></a> [alert\_policies](#input\_alert\_policies) | n/a | <pre>map(object({<br/>    display_name = string<br/>    severity     = string<br/>    condition = object({<br/>      filter          = string<br/>      comparison      = string<br/>      threshold_value = number<br/>      duration        = string<br/>      aligner         = string<br/>    })<br/>    documentation = string<br/>  }))</pre> | n/a | yes |
| <a name="input_cluster_name"></a> [cluster\_name](#input\_cluster\_name) | n/a | `string` | n/a | yes |
| <a name="input_default_notification_channels"></a> [default\_notification\_channels](#input\_default\_notification\_channels) | n/a | `set(string)` | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | n/a | `map(string)` | `{}` | no |
| <a name="input_metrics_scope_project"></a> [metrics\_scope\_project](#input\_metrics\_scope\_project) | n/a | `string` | n/a | yes |
| <a name="input_notification_channels"></a> [notification\_channels](#input\_notification\_channels) | n/a | <pre>map(object({<br/>    type  = string<br/>    email = optional(string)<br/>  }))</pre> | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | n/a | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_alert_policy_names"></a> [alert\_policy\_names](#output\_alert\_policy\_names) | n/a |
| <a name="output_metrics_scope_contract"></a> [metrics\_scope\_contract](#output\_metrics\_scope\_contract) | Existing scope read by these policies; project\_factory remains the sole membership owner. |
| <a name="output_notification_channel_names"></a> [notification\_channel\_names](#output\_notification\_channel\_names) | n/a |
<!-- END_TF_DOCS -->
