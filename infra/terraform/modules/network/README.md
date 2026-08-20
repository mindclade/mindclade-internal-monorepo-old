# GCP network baseline

This module creates one custom-mode IPv4 VPC, protected private subnets, an
explicit default-internet-gateway route, and optional subnet-scoped Public Cloud
NAT gateways. Private Google Access is mandatory and VPC Flow Logs are enabled by default.
Cloud NAT always logs and can use Google-managed or caller-provided addresses.
Manual addresses must be canonical regional Compute Address self-links in this
project, and router/NAT identities are unique within a region.

```hcl
module "network" {
  source = "../../modules/network"

  networks = {
    production = {
      project_id        = "mindclade-network-prod"
      network_name      = "mindclade-prod"
      primary_subnet_key = "nodes"
      subnets = {
        nodes = {
          region        = "us-central1"
          ip_cidr_range = "10.20.0.0/20"
          secondary_ip_ranges = {
            prod-pods     = "10.24.0.0/14"
            prod-services = "10.28.0.0/20"
          }
        }
      }
      nat_gateways = {
        central = {
          region      = "us-central1"
          router_name = "mindclade-prod-central-router"
          nat_name    = "mindclade-prod-central-nat"
          subnet_keys = ["nodes"]
        }
      }
    }
  }
}
```

The module deletes the provider-created default route and recreates it as a
Terraform-owned, deletion-protected resource. Set
`create_default_internet_route = false` only for a PSC-only or otherwise
explicitly routed network; Public Cloud NAT is then rejected. The route alone
does not grant an external IP or NAT access.

Firewall policies/rules, Shared VPC host and service-project attachment, PSC,
hybrid connectivity, IPv6, DNS policy, and IPAM allocation authority remain
separate lifecycle boundaries. Callers must validate CIDR overlap, egress policy,
quota, NAT capacity, and connectivity at representative scale. Destruction
requires an explicit code change removing both provider and Terraform guards.
This repository module is a baseline, not evidence of a deployed or qualified
production network.

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
| <a name="input_networks"></a> [networks](#input\_networks) | VPC networks keyed by a stable name — in this estate, the environment.<br/><br/>A map rather than a single network because `3-networks/` sits above the environment level:<br/>one unit builds development, staging, and production so that the subnet layout cannot<br/>drift between them. Every output here is therefore keyed the same way, and a consumer in<br/>`5-workloads` indexes it with its own environment.<br/><br/>Splitting this into one module call per environment would mean three copies of the same<br/>peering, NAT, and subnet logic kept in step by hand, and the failure when they drift is an<br/>address collision discovered the day someone peers two of them. | <pre>map(object({<br/>    project_id   = string<br/>    network_name = string<br/>    description  = optional(string, "Mindclade Shared VPC managed by Terraform.")<br/><br/>    routing_mode                      = optional(string, "REGIONAL")<br/>    mtu                               = optional(number, 1460)<br/>    firewall_policy_enforcement_order = optional(string, "AFTER_CLASSIC_FIREWALL")<br/><br/>    # Which subnet GKE's nodes take addresses from. Named rather than inferred: several<br/>    # subnets in this network are not node subnets, and picking one by position would make<br/>    # adding a subnet silently move the cluster.<br/>    #<br/>    # Its secondary ranges are matched by suffix — `*-pods` and `*-services` — to produce the<br/>    # `pods_range_names` and `services_range_names` outputs the GKE units consume. The suffix<br/>    # is a convention, so it is validated below rather than left to fail at cluster creation<br/>    # with an error naming the range and not this file.<br/>    primary_subnet_key = optional(string, "nodes")<br/><br/>    # The default 0.0.0.0/0 route is deleted at creation and replaced by this one, so that<br/>    # egress has exactly one path and it appears in flow logs with a stable source address.<br/>    # Public Cloud NAT requires it — there is a precondition below that says so.<br/>    create_default_internet_route   = optional(bool, true)<br/>    default_internet_route_priority = optional(number, 1000)<br/><br/>    subnets = map(object({<br/>      region        = string<br/>      ip_cidr_range = string<br/>      description   = optional(string, "Mindclade private subnet managed by Terraform.")<br/><br/>      # PRIVATE for workload subnets. REGIONAL_MANAGED_PROXY is the proxy-only subnet a<br/>      # regional internal Application Load Balancer (gke-l7-rilb) reserves its Envoy fleet<br/>      # from — it holds proxies, never addresses, so nothing can be allocated from it and a<br/>      # Gateway VIP must come from a PRIVATE subnet instead.<br/>      purpose = optional(string, "PRIVATE")<br/><br/>      # ACTIVE or BACKUP, and only meaningful for REGIONAL_MANAGED_PROXY. Exactly one ACTIVE<br/>      # proxy-only subnet may exist per region per network; a second ACTIVE one is rejected by<br/>      # the API with an error that names neither subnet.<br/>      role = optional(string)<br/><br/>      secondary_ip_ranges = optional(map(string), {})<br/><br/>      flow_logs = optional(object({<br/>        enabled              = optional(bool, true)<br/>        aggregation_interval = optional(string, "INTERVAL_5_MIN")<br/>        sampling             = optional(number, 0.5)<br/>        filter               = optional(string, "true")<br/>      }), {})<br/>    }))<br/><br/>    nat_gateways = optional(map(object({<br/>      region                 = string<br/>      router_name            = string<br/>      nat_name               = string<br/>      subnet_keys            = set(string)<br/>      nat_ip_allocate_option = optional(string, "AUTO_ONLY")<br/>      nat_ips                = optional(list(string), [])<br/>      min_ports_per_vm       = optional(number, 64)<br/>      log_filter             = optional(string, "ERRORS_ONLY")<br/>    })), {})<br/>  }))</pre> | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_host_project_ids"></a> [host\_project\_ids](#output\_host\_project\_ids) | Shared VPC host project id by network key. The cluster is a service project; this is the project its network lives in. |
| <a name="output_nat_gateway_names"></a> [nat\_gateway\_names](#output\_nat\_gateway\_names) | Cloud NAT gateway names, nested network key → gateway key. |
| <a name="output_network_id"></a> [network\_id](#output\_network\_id) | VPC network id by network key. |
| <a name="output_network_name"></a> [network\_name](#output\_network\_name) | VPC network name by network key. |
| <a name="output_network_self_link"></a> [network\_self\_link](#output\_network\_self\_link) | VPC network self-link by network key. This is what Cloud DNS private zones attach to. |
| <a name="output_pods_range_names"></a> [pods\_range\_names](#output\_pods\_range\_names) | GKE pod secondary range name by network key. Passed straight through as ip\_range\_pods. |
| <a name="output_proxy_only_subnet_ranges"></a> [proxy\_only\_subnet\_ranges](#output\_proxy\_only\_subnet\_ranges) | CIDRs of the REGIONAL\_MANAGED\_PROXY subnets, by network key.<br/><br/>This is the source range a regional internal Application Load Balancer's proxies connect<br/>from. Backend firewall rules and NetworkPolicy must allow it; without that the load<br/>balancer's health checks fail and every backend reports unhealthy with nothing in the<br/>workload's own logs. |
| <a name="output_services_range_names"></a> [services\_range\_names](#output\_services\_range\_names) | GKE service secondary range name by network key. Passed straight through as ip\_range\_services. |
| <a name="output_subnet_ip_cidr_ranges"></a> [subnet\_ip\_cidr\_ranges](#output\_subnet\_ip\_cidr\_ranges) | Subnet primary CIDRs, nested network key → subnet key. Firewall rules scope to these rather than restating them. |
| <a name="output_subnet_secondary_ranges"></a> [subnet\_secondary\_ranges](#output\_subnet\_secondary\_ranges) | Secondary range names by network key → subnet key. GKE consumes these as ip\_range\_pods and ip\_range\_services. |
| <a name="output_subnet_self_links"></a> [subnet\_self\_links](#output\_subnet\_self\_links) | Subnet self-links, nested network key → subnet key.<br/><br/>Nested rather than flat, because a flat "development/nodes" key would push string-splitting<br/>onto every consumer. |
| <a name="output_subnetwork_names"></a> [subnetwork\_names](#output\_subnetwork\_names) | Node subnet name by network key — the network's primary\_subnet\_key. |
<!-- END_TF_DOCS -->
