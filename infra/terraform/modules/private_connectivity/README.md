# Private connectivity module

Separates Private Service Access, consumer Google API endpoints, and producer service attachments
from general VPC ownership. Service attachments require manual consumer allowlists and cannot be
created without a forwarding-rule target and PSC NAT subnets.

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
| <a name="input_google_api_endpoints"></a> [google\_api\_endpoints](#input\_google\_api\_endpoints) | Optional consumer PSC endpoints for Google APIs. | <pre>map(object({<br/>    project_id = string<br/>    region     = string<br/>    network    = string<br/>    subnetwork = string<br/>    target     = string<br/>    address    = string<br/>  }))</pre> | `{}` | no |
| <a name="input_labels"></a> [labels](#input\_labels) | n/a | `map(string)` | `{}` | no |
| <a name="input_service_attachments"></a> [service\_attachments](#input\_service\_attachments) | Producer service attachments admitted only with explicit targets, NAT subnets, and consumers. | <pre>map(object({<br/>    project_id            = string<br/>    region                = string<br/>    target_service        = string<br/>    nat_subnets           = list(string)<br/>    accepted_project_ids  = map(number)<br/>    enable_proxy_protocol = optional(bool, false)<br/>  }))</pre> | `{}` | no |
| <a name="input_service_networking"></a> [service\_networking](#input\_service\_networking) | Private Service Access ranges keyed by environment. | <pre>map(object({<br/>    project_id      = string<br/>    network         = string<br/>    allocated_range = string<br/>  }))</pre> | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_google_api_forwarding_rules"></a> [google\_api\_forwarding\_rules](#output\_google\_api\_forwarding\_rules) | n/a |
| <a name="output_service_attachment_self_links"></a> [service\_attachment\_self\_links](#output\_service\_attachment\_self\_links) | n/a |
| <a name="output_service_networking_ranges"></a> [service\_networking\_ranges](#output\_service\_networking\_ranges) | Reserved service-networking range names keyed by environment. |
<!-- END_TF_DOCS -->
