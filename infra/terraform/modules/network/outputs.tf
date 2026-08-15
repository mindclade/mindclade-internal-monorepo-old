# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# Every output is keyed by NETWORK KEY — the environment, in this estate. A consumer in
# 5-workloads indexes it with its own environment:
#
#   network = dependency.vpc.outputs.network_self_link[include.envcommon.locals.environment]
#
# A scalar here would mean one VPC shared by development and production, which is precisely
# the boundary the rest of this estate is built to hold.

output "network_id" {
  description = "VPC network id by network key."
  value       = { for k, n in google_compute_network.this : k => n.id }
}

output "network_name" {
  description = "VPC network name by network key."
  value       = { for k, n in google_compute_network.this : k => n.name }
}

output "network_self_link" {
  description = "VPC network self-link by network key. This is what Cloud DNS private zones attach to."
  value       = { for k, n in google_compute_network.this : k => n.self_link }
}

output "subnet_self_links" {
  description = <<-EOT
    Subnet self-links, nested network key → subnet key.

    Nested rather than flat, because a flat "development/nodes" key would push string-splitting
    onto every consumer.
  EOT
  value = {
    for net_key in keys(var.networks) : net_key => {
      for subnet_key in keys(var.networks[net_key].subnets) :
      subnet_key => google_compute_subnetwork.this["${net_key}/${subnet_key}"].self_link
    }
  }
}

output "subnet_ip_cidr_ranges" {
  description = "Subnet primary CIDRs, nested network key → subnet key. Firewall rules scope to these rather than restating them."
  value = {
    for net_key in keys(var.networks) : net_key => {
      for subnet_key in keys(var.networks[net_key].subnets) :
      subnet_key => google_compute_subnetwork.this["${net_key}/${subnet_key}"].ip_cidr_range
    }
  }
}

output "proxy_only_subnet_ranges" {
  description = <<-EOT
    CIDRs of the REGIONAL_MANAGED_PROXY subnets, by network key.

    This is the source range a regional internal Application Load Balancer's proxies connect
    from. Backend firewall rules and NetworkPolicy must allow it; without that the load
    balancer's health checks fail and every backend reports unhealthy with nothing in the
    workload's own logs.
  EOT
  value = {
    for net_key, net in var.networks : net_key => [
      for subnet_key, subnet in net.subnets :
      google_compute_subnetwork.this["${net_key}/${subnet_key}"].ip_cidr_range
      if subnet.purpose == "REGIONAL_MANAGED_PROXY"
    ]
  }
}

output "subnet_secondary_ranges" {
  description = "Secondary range names by network key → subnet key. GKE consumes these as ip_range_pods and ip_range_services."
  value = {
    for net_key in keys(var.networks) : net_key => {
      for subnet_key in keys(var.networks[net_key].subnets) :
      subnet_key => keys(var.networks[net_key].subnets[subnet_key].secondary_ip_ranges)
    }
  }
}

# ---------------------------------------------------------------------------------------
# Flat, environment-keyed outputs for the GKE units
# ---------------------------------------------------------------------------------------
# The nested maps above are the general interface. These four are the specific one that
# 5-workloads/<env>/gke consumes, and they exist so that a cluster unit reads
#
#   subnetwork = dependency.vpc.outputs.subnetwork_names[environment]
#
# rather than reaching two levels into a structure and restating which subnet is the node
# subnet. Which one that is, and which secondary ranges carry pods and services, is declared
# once per network as `primary_subnet_key` and validated there.

output "host_project_ids" {
  description = "Shared VPC host project id by network key. The cluster is a service project; this is the project its network lives in."
  value       = { for k, n in var.networks : k => n.project_id }
}

output "subnetwork_names" {
  description = "Node subnet name by network key — the network's primary_subnet_key."
  value       = { for k, n in var.networks : k => n.primary_subnet_key }
}

output "pods_range_names" {
  description = "GKE pod secondary range name by network key. Passed straight through as ip_range_pods."
  value = {
    for net_key, net in var.networks : net_key => one([
      for name, _ in net.subnets[net.primary_subnet_key].secondary_ip_ranges : name
      if endswith(name, "-pods")
    ])
  }
}

output "services_range_names" {
  description = "GKE service secondary range name by network key. Passed straight through as ip_range_services."
  value = {
    for net_key, net in var.networks : net_key => one([
      for name, _ in net.subnets[net.primary_subnet_key].secondary_ip_ranges : name
      if endswith(name, "-services")
    ])
  }
}

output "nat_gateway_names" {
  description = "Cloud NAT gateway names, nested network key → gateway key."
  value = {
    for net_key, net in var.networks : net_key => {
      for gateway_key in keys(net.nat_gateways) :
      gateway_key => google_compute_router_nat.this["${net_key}/${gateway_key}"].name
    }
  }
}
