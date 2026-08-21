# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

mock_provider "google" {}
run "circuits_start_disabled" {
  command = plan
  variables {
    project_id = "mindclade-production-net"
    network    = "projects/mock/global/networks/production"
    region     = "us-central1"
    interconnects = {
      primary   = { name = "mindclade-primary", location = "las-zone1-1", description = "primary", link_type = "LINK_TYPE_ETHERNET_10G_LR", requested_link_count = 1 }
      secondary = { name = "mindclade-secondary", location = "las-zone2-1", description = "secondary", link_type = "LINK_TYPE_ETHERNET_10G_LR", requested_link_count = 1 }
    }
    cloud_router           = { name = "mindclade-router", asn = 64512, advertise_mode = "CUSTOM", advertised_ip_ranges = [{ range = "10.48.0.0/16", description = "production" }] }
    bgp_md5_authentication = true
    macsec_enabled         = true
  }
  assert {
    condition     = alltrue([for circuit in google_compute_interconnect.this : !circuit.admin_enabled])
    error_message = "Physical circuits must remain disabled before connected qualification."
  }
}
