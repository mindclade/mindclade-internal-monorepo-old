# GCP private services access baseline

This module reserves one internal IPv4 block on a custom-mode VPC and peers the
network with `servicenetworking.googleapis.com`. Managed services that attach
through private services access, such as Cloud SQL and Memorystore, carve their
service subnets out of the reserved block, so the range must never overlap
subnets, secondary ranges, or other reserved blocks in the same routing domain.

```hcl
module "private_service_access" {
  source = "../../modules/private_service_access"

  project_id          = "mindclade-production"
  network_id          = module.network.network_id["production"]
  reserved_range_name = "mindclade-production-psa"
  address             = "10.41.0.0"
  prefix_length       = 16
}
```

The reserved range and the peering connection carry Terraform lifecycle
protection, and the connection uses the ABANDON deletion policy because tearing
down an in-use peering strands every attached service instance. Custom-route
exchange with the producer network stays disabled unless a caller explicitly
enables it, for example so cross-region Cloud SQL replicas stay reachable over
hybrid connectivity.

Deleting or shrinking the range, moving it to another network, or removing the
peering while instances exist are outage-grade operations that require a
reviewed migration plan. The Service Networking API must be enabled on the
project, and the applying identity needs `servicenetworking.services.addPeering`
plus Compute address permissions. This module is a repository baseline, not
evidence that a live network is peered or qualified.

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
| <a name="input_address"></a> [address](#input\_address) | First IPv4 address of the reserved range; leave empty to let Google choose a free block | `string` | `""` | no |
| <a name="input_export_custom_routes"></a> [export\_custom\_routes](#input\_export\_custom\_routes) | Export custom routes to the service producer network, for example for cross-region replica reachability over hybrid connectivity | `bool` | `false` | no |
| <a name="input_import_custom_routes"></a> [import\_custom\_routes](#input\_import\_custom\_routes) | Import custom routes advertised by the service producer network | `bool` | `false` | no |
| <a name="input_labels"></a> [labels](#input\_labels) | Labels applied to the reserved range | `map(string)` | `{}` | no |
| <a name="input_network_id"></a> [network\_id](#input\_network\_id) | Fully qualified resource ID of the VPC network to peer with Google managed services | `string` | n/a | yes |
| <a name="input_prefix_length"></a> [prefix\_length](#input\_prefix\_length) | Prefix length of the reserved range; managed services carve service subnets out of this block | `number` | `20` | no |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Google Cloud project that owns the VPC network and the reserved peering range | `string` | n/a | yes |
| <a name="input_reserved_range_description"></a> [reserved\_range\_description](#input\_reserved\_range\_description) | Purpose and ownership of the reserved managed-services range | `string` | `"Reserved range for Google managed services reached over private services access."` | no |
| <a name="input_reserved_range_name"></a> [reserved\_range\_name](#input\_reserved\_range\_name) | Name of the reserved internal range allocated to Google managed services | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_connection_id"></a> [connection\_id](#output\_connection\_id) | Identifier of the service networking connection |
| <a name="output_peering_name"></a> [peering\_name](#output\_peering\_name) | Name of the VPC peering created by the service networking connection |
| <a name="output_reserved_range_cidr"></a> [reserved\_range\_cidr](#output\_reserved\_range\_cidr) | Reserved managed-services block in CIDR notation |
| <a name="output_reserved_range_name"></a> [reserved\_range\_name](#output\_reserved\_range\_name) | Name of the reserved range; pass to Cloud SQL allocated\_ip\_range and Memorystore reserved\_ip\_range consumers |
<!-- END_TF_DOCS -->
