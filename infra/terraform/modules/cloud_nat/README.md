# Cloud NAT module

Creates one or more regional Cloud Routers and NAT gateways for existing private VPCs. Manual
allocation is the default and provisions protected premium-tier addresses so downstream allowlists
do not drift. Dynamic port allocation, explicit bounds, reduced idle timeouts, and error logging
are part of the interface rather than console-only settings.

The caller owns the VPC, routes, firewall egress policy, and subnet lifecycle. This module never
creates a network or silently switches from static to Google-managed source addresses.

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
| <a name="input_nats"></a> [nats](#input\_nats) | Cloud NAT gateways keyed by environment or another stable ownership key. | <pre>map(object({<br/>    project_id  = string<br/>    network     = string<br/>    region      = string<br/>    router_name = string<br/>    nat_name    = string<br/><br/>    nat_ip_allocate_option             = optional(string, "MANUAL_ONLY")<br/>    static_ip_count                    = optional(number, 2)<br/>    source_subnetwork_ip_ranges_to_nat = optional(string, "ALL_SUBNETWORKS_ALL_IP_RANGES")<br/>    min_ports_per_vm                   = optional(number, 64)<br/>    enable_dynamic_port_allocation     = optional(bool, true)<br/>    max_ports_per_vm                   = optional(number, 512)<br/>    tcp_established_idle_timeout_sec   = optional(number, 1200)<br/>    tcp_transitory_idle_timeout_sec    = optional(number, 30)<br/>    udp_idle_timeout_sec               = optional(number, 30)<br/>    icmp_idle_timeout_sec              = optional(number, 30)<br/>    log_config = optional(object({<br/>      enable = optional(bool, true)<br/>      filter = optional(string, "ERRORS_ONLY")<br/>    }), {})<br/>  }))</pre> | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_external_addresses"></a> [external\_addresses](#output\_external\_addresses) | Stable external NAT addresses keyed by ownership key and ordinal. |
| <a name="output_nat_names"></a> [nat\_names](#output\_nat\_names) | Cloud NAT names keyed by caller ownership key. |
| <a name="output_router_names"></a> [router\_names](#output\_router\_names) | Cloud Router names keyed by caller ownership key. |
<!-- END_TF_DOCS -->
