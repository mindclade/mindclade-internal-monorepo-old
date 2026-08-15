# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

resource "google_compute_global_address" "reserved" {
  project       = var.project_id
  name          = var.reserved_range_name
  description   = var.reserved_range_description
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  ip_version    = "IPV4"
  address       = var.address == "" ? null : var.address
  prefix_length = var.prefix_length
  network       = var.network_id
  labels        = var.labels

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_service_networking_connection" "this" {
  network                 = var.network_id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.reserved.name]
  deletion_policy         = "ABANDON"

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_compute_network_peering_routes_config" "servicenetworking" {
  count = var.export_custom_routes || var.import_custom_routes ? 1 : 0

  project              = var.project_id
  network              = element(reverse(split("/", var.network_id)), 0)
  peering              = "servicenetworking-googleapis-com"
  export_custom_routes = var.export_custom_routes
  import_custom_routes = var.import_custom_routes

  depends_on = [google_service_networking_connection.this]
}
