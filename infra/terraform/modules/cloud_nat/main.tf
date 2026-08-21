# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  static_ips = merge([
    for nat_key, nat in var.nats : {
      for index in range(nat.static_ip_count) : "${nat_key}/${index}" => {
        nat_key    = nat_key
        project_id = nat.project_id
        region     = nat.region
        name       = "${nat.nat_name}-${index + 1}"
      }
      if nat.nat_ip_allocate_option == "MANUAL_ONLY"
    }
  ]...)
}

resource "google_compute_address" "nat" {
  for_each = local.static_ips

  project      = each.value.project_id
  region       = each.value.region
  name         = each.value.name
  address_type = "EXTERNAL"
  network_tier = "PREMIUM"

  deletion_policy = "PREVENT"
  lifecycle {
    prevent_destroy = true
  }
}

resource "google_compute_router" "nat" {
  for_each = var.nats

  project = each.value.project_id
  region  = each.value.region
  name    = each.value.router_name
  network = each.value.network
}

resource "google_compute_router_nat" "nat" {
  for_each = var.nats

  project                            = each.value.project_id
  region                             = each.value.region
  name                               = each.value.nat_name
  router                             = google_compute_router.nat[each.key].name
  nat_ip_allocate_option             = each.value.nat_ip_allocate_option
  source_subnetwork_ip_ranges_to_nat = each.value.source_subnetwork_ip_ranges_to_nat
  nat_ips = each.value.nat_ip_allocate_option == "MANUAL_ONLY" ? [
    for key, address in google_compute_address.nat : address.self_link
    if startswith(key, "${each.key}/")
  ] : null

  min_ports_per_vm                 = each.value.min_ports_per_vm
  enable_dynamic_port_allocation   = each.value.enable_dynamic_port_allocation
  max_ports_per_vm                 = each.value.enable_dynamic_port_allocation ? each.value.max_ports_per_vm : null
  tcp_established_idle_timeout_sec = each.value.tcp_established_idle_timeout_sec
  tcp_transitory_idle_timeout_sec  = each.value.tcp_transitory_idle_timeout_sec
  udp_idle_timeout_sec             = each.value.udp_idle_timeout_sec
  icmp_idle_timeout_sec            = each.value.icmp_idle_timeout_sec

  log_config {
    enable = each.value.log_config.enable
    filter = each.value.log_config.filter
  }
}
