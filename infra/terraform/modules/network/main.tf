# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  # network_key/subnet_key → the pair, flattened so one resource block covers every subnet in
  # every network. The composite key has to be stable across plans, so it is built from the
  # two map keys rather than from anything the provider computes.
  subnets = merge([
    for net_key, net in var.networks : {
      for subnet_key, subnet in net.subnets :
      "${net_key}/${subnet_key}" => merge(subnet, {
        net_key    = net_key
        subnet_key = subnet_key
        project_id = net.project_id
      })
    }
  ]...)

  nat_gateways = merge([
    for net_key, net in var.networks : {
      for gateway_key, gateway in net.nat_gateways :
      "${net_key}/${gateway_key}" => merge(gateway, {
        net_key    = net_key
        project_id = net.project_id
      })
    }
  ]...)

  networks_with_default_route = {
    for net_key, net in var.networks : net_key => net
    if net.create_default_internet_route
  }

  # A subnet may be attached to at most one Cloud NAT gateway per region. Two gateways
  # claiming the same subnet is accepted by Terraform and rejected by the API, so it is
  # checked here where both claims are visible at once.
  nat_subnet_assignments = flatten([
    for net_key, net in var.networks : [
      for gateway in values(net.nat_gateways) : [
        for subnet_key in gateway.subnet_keys : "${net_key}/${gateway.region}/${subnet_key}"
      ]
    ]
  ])
}

resource "google_compute_network" "this" {
  for_each = var.networks

  project                                   = each.value.project_id
  name                                      = each.value.network_name
  description                               = each.value.description
  auto_create_subnetworks                   = false
  delete_default_routes_on_create           = true
  mtu                                       = each.value.mtu
  routing_mode                              = each.value.routing_mode
  network_firewall_policy_enforcement_order = each.value.firewall_policy_enforcement_order
  deletion_policy                           = "PREVENT"

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = length(local.nat_subnet_assignments) == length(distinct(local.nat_subnet_assignments))
      error_message = "A subnet can be assigned to at most one Cloud NAT gateway in a region."
    }
  }
}

resource "google_compute_route" "default_internet" {
  for_each = local.networks_with_default_route

  project          = each.value.project_id
  name             = "${substr(each.value.network_name, 0, 45)}-default-internet"
  description      = "Explicit default route for Private Google Access and approved internet egress."
  network          = google_compute_network.this[each.key].self_link
  dest_range       = "0.0.0.0/0"
  next_hop_gateway = "default-internet-gateway"
  priority         = each.value.default_internet_route_priority
  deletion_policy  = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_compute_subnetwork" "this" {
  #checkov:skip=CKV_GCP_74: PRIVATE subnets force Private Google Access; proxy-only subnets reject that API field.
  for_each = local.subnets

  project       = each.value.project_id
  name          = each.value.subnet_key
  description   = each.value.description
  region        = each.value.region
  network       = google_compute_network.this[each.value.net_key].id
  ip_cidr_range = each.value.ip_cidr_range
  purpose       = each.value.purpose
  role          = each.value.role
  stack_type    = "IPV4_ONLY"

  # Private Google Access is a workload-subnet property. A proxy-only subnet has no instances
  # in it to grant it to, and setting it there is rejected by the API.
  private_ip_google_access = each.value.purpose == "PRIVATE"

  deletion_policy = "PREVENT"

  dynamic "secondary_ip_range" {
    for_each = each.value.secondary_ip_ranges
    content {
      range_name    = secondary_ip_range.key
      ip_cidr_range = secondary_ip_range.value
    }
  }

  dynamic "log_config" {
    for_each = each.value.flow_logs.enabled ? [each.value.flow_logs] : []
    content {
      aggregation_interval = log_config.value.aggregation_interval
      flow_sampling        = log_config.value.sampling
      metadata             = "INCLUDE_ALL_METADATA"
      filter_expr          = log_config.value.filter
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_compute_router" "nat" {
  for_each = local.nat_gateways

  project         = each.value.project_id
  name            = each.value.router_name
  description     = "Cloud Router control plane for ${each.value.nat_name}."
  network         = google_compute_network.this[each.value.net_key].id
  region          = each.value.region
  deletion_policy = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_compute_router_nat" "this" {
  for_each = local.nat_gateways

  project                             = each.value.project_id
  name                                = each.value.nat_name
  region                              = each.value.region
  router                              = google_compute_router.nat[each.key].name
  type                                = "PUBLIC"
  nat_ip_allocate_option              = each.value.nat_ip_allocate_option
  nat_ips                             = each.value.nat_ip_allocate_option == "MANUAL_ONLY" ? each.value.nat_ips : null
  source_subnetwork_ip_ranges_to_nat  = "LIST_OF_SUBNETWORKS"
  min_ports_per_vm                    = each.value.min_ports_per_vm
  enable_endpoint_independent_mapping = true
  deletion_policy                     = "PREVENT"

  dynamic "subnetwork" {
    for_each = each.value.subnet_keys
    content {
      name                    = google_compute_subnetwork.this["${each.value.net_key}/${subnetwork.value}"].self_link
      source_ip_ranges_to_nat = ["ALL_IP_RANGES"]
    }
  }

  log_config {
    enable = true
    filter = each.value.log_filter
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = var.networks[each.value.net_key].create_default_internet_route
      error_message = "Public Cloud NAT requires the module-managed default internet gateway route."
    }

    # NAT a proxy-only subnet and the apply fails at the API with an error about the subnet's
    # purpose. Caught here because the two facts — which subnets a gateway claims, and what
    # each subnet is for — live in different parts of the input.
    precondition {
      condition = alltrue([
        for subnet_key in each.value.subnet_keys :
        try(
          var.networks[each.value.net_key].subnets[subnet_key].region == each.value.region &&
          var.networks[each.value.net_key].subnets[subnet_key].purpose == "PRIVATE",
          false,
        )
      ])
      error_message = "Every Cloud NAT subnet key must exist in the same network, be in the gateway's region, and have purpose PRIVATE."
    }
  }
}
