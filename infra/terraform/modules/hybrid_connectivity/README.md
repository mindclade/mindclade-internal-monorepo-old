# Hybrid connectivity module

Creates deletion-protected Dedicated Interconnect circuit reservations and their custom-advertisement
Cloud Router. Circuits always start administratively disabled. Attachment, peer, and secret-backed
BGP MD5/MACsec details are intentionally an explicit activation blocker; the excluded live unit must
not be enabled until those connected inputs and colocation work exist.

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
| <a name="input_bgp_md5_authentication"></a> [bgp\_md5\_authentication](#input\_bgp\_md5\_authentication) | n/a | `bool` | n/a | yes |
| <a name="input_cloud_router"></a> [cloud\_router](#input\_cloud\_router) | n/a | <pre>object({<br/>    name           = string<br/>    asn            = number<br/>    advertise_mode = string<br/>    advertised_ip_ranges = set(object({<br/>      range       = string<br/>      description = string<br/>    }))<br/>  })</pre> | n/a | yes |
| <a name="input_interconnects"></a> [interconnects](#input\_interconnects) | n/a | <pre>map(object({<br/>    name                 = string<br/>    location             = string<br/>    description          = string<br/>    link_type            = string<br/>    requested_link_count = number<br/>  }))</pre> | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | n/a | `map(string)` | `{}` | no |
| <a name="input_macsec_enabled"></a> [macsec\_enabled](#input\_macsec\_enabled) | n/a | `bool` | n/a | yes |
| <a name="input_network"></a> [network](#input\_network) | n/a | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | n/a | `string` | n/a | yes |
| <a name="input_region"></a> [region](#input\_region) | n/a | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_activation_contract"></a> [activation\_contract](#output\_activation\_contract) | Controls that protected attachment/BGP activation must supply with reviewed keys and peer coordinates. |
| <a name="output_interconnect_self_links"></a> [interconnect\_self\_links](#output\_interconnect\_self\_links) | n/a |
| <a name="output_router_name"></a> [router\_name](#output\_router\_name) | n/a |
<!-- END_TF_DOCS -->
