# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

run "reserved_peering_contract" {
  command = plan

  variables {
    project_id          = "mindclade-production"
    network_id          = "projects/mindclade-production/global/networks/mindclade-production"
    reserved_range_name = "mindclade-production-psa"
    address             = "10.41.0.0"
    prefix_length       = 16
  }

  assert {
    condition = (
      google_compute_global_address.reserved.purpose == "VPC_PEERING" &&
      google_compute_global_address.reserved.address_type == "INTERNAL" &&
      google_compute_global_address.reserved.address == "10.41.0.0" &&
      google_compute_global_address.reserved.prefix_length == 16 &&
      google_compute_global_address.reserved.network == "projects/mindclade-production/global/networks/mindclade-production"
    )
    error_message = "The reserved block must be an internal VPC peering range on the target network."
  }

  assert {
    condition = (
      google_service_networking_connection.this.service == "servicenetworking.googleapis.com" &&
      google_service_networking_connection.this.reserved_peering_ranges == tolist(["mindclade-production-psa"]) &&
      google_service_networking_connection.this.deletion_policy == "ABANDON"
    )
    error_message = "The peering must hand exactly the reserved range to the service producer and abandon on destroy."
  }

  assert {
    condition     = length(google_compute_network_peering_routes_config.servicenetworking) == 0
    error_message = "Custom-route exchange with the producer network must stay disabled by default."
  }
}

run "automatic_address_selection" {
  command = plan

  variables {
    project_id          = "mindclade-development"
    network_id          = "projects/mindclade-development/global/networks/mindclade-development"
    reserved_range_name = "mindclade-development-psa"
  }

  assert {
    condition     = google_compute_global_address.reserved.prefix_length == 20
    error_message = "The default reserved block must be a /20."
  }
}

run "custom_route_exchange_is_explicit" {
  command = plan

  variables {
    project_id           = "mindclade-production"
    network_id           = "projects/mindclade-production/global/networks/mindclade-production"
    reserved_range_name  = "mindclade-production-psa"
    export_custom_routes = true
  }

  assert {
    condition = (
      google_compute_network_peering_routes_config.servicenetworking[0].peering == "servicenetworking-googleapis-com" &&
      google_compute_network_peering_routes_config.servicenetworking[0].network == "mindclade-production" &&
      google_compute_network_peering_routes_config.servicenetworking[0].export_custom_routes == true &&
      google_compute_network_peering_routes_config.servicenetworking[0].import_custom_routes == false
    )
    error_message = "Enabling route exchange must configure only the requested direction on the producer peering."
  }
}

run "rejects_non_canonical_network" {
  command = plan

  variables {
    project_id          = "mindclade-production"
    network_id          = "mindclade-production"
    reserved_range_name = "mindclade-production-psa"
  }

  expect_failures = [var.network_id]
}

run "rejects_oversized_block" {
  command = plan

  variables {
    project_id          = "mindclade-production"
    network_id          = "projects/mindclade-production/global/networks/mindclade-production"
    reserved_range_name = "mindclade-production-psa"
    prefix_length       = 8
  }

  expect_failures = [var.prefix_length]
}
