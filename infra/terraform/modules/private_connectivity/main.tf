# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

resource "google_compute_global_address" "service_networking" {
  for_each = var.service_networking

  project         = each.value.project_id
  name            = "${each.key}-service-networking"
  purpose         = "VPC_PEERING"
  address_type    = "INTERNAL"
  network         = each.value.network
  address         = split("/", each.value.allocated_range)[0]
  prefix_length   = tonumber(split("/", each.value.allocated_range)[1])
  labels          = var.labels
  deletion_policy = "PREVENT"
}

resource "google_service_networking_connection" "this" {
  for_each = var.service_networking

  network                 = each.value.network
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.service_networking[each.key].name]
  deletion_policy         = "PREVENT"
}

resource "google_compute_address" "google_api" {
  for_each = var.google_api_endpoints

  project         = each.value.project_id
  region          = each.value.region
  name            = each.key
  subnetwork      = each.value.subnetwork
  address_type    = "INTERNAL"
  address         = each.value.address
  purpose         = "GCE_ENDPOINT"
  labels          = var.labels
  deletion_policy = "PREVENT"
}

resource "google_compute_forwarding_rule" "google_api" {
  for_each = var.google_api_endpoints

  project               = each.value.project_id
  region                = each.value.region
  name                  = each.key
  network               = each.value.network
  subnetwork            = each.value.subnetwork
  ip_address            = google_compute_address.google_api[each.key].id
  target                = each.value.target
  load_balancing_scheme = ""
  deletion_policy       = "PREVENT"
}

resource "google_compute_service_attachment" "this" {
  for_each = var.service_attachments

  project               = each.value.project_id
  region                = each.value.region
  name                  = each.key
  target_service        = each.value.target_service
  nat_subnets           = each.value.nat_subnets
  connection_preference = "ACCEPT_MANUAL"
  enable_proxy_protocol = each.value.enable_proxy_protocol
  deletion_policy       = "PREVENT"

  dynamic "consumer_accept_lists" {
    for_each = each.value.accepted_project_ids
    content {
      project_id_or_num = consumer_accept_lists.key
      connection_limit  = consumer_accept_lists.value
    }
  }
}
