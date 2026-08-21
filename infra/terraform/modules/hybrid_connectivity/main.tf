# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

resource "google_compute_interconnect" "this" {
  for_each = var.interconnects

  project              = var.project_id
  name                 = each.value.name
  description          = each.value.description
  interconnect_type    = "DEDICATED"
  link_type            = each.value.link_type
  location             = each.value.location
  requested_link_count = each.value.requested_link_count
  labels               = merge(var.labels, { managed-by = "terraform" })
  admin_enabled        = false
  deletion_policy      = "PREVENT"

  lifecycle { prevent_destroy = true }
}

resource "google_compute_router" "this" {
  project         = var.project_id
  region          = var.region
  name            = var.cloud_router.name
  network         = var.network
  deletion_policy = "PREVENT"

  bgp {
    asn            = var.cloud_router.asn
    advertise_mode = var.cloud_router.advertise_mode
    dynamic "advertised_ip_ranges" {
      for_each = var.cloud_router.advertised_ip_ranges
      content {
        range       = advertised_ip_ranges.value.range
        description = advertised_ip_ranges.value.description
      }
    }
  }
  lifecycle { prevent_destroy = true }
}
