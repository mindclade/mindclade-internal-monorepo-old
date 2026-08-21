# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

run "protected_manual_nat" {
  command = plan
  variables {
    nats = {
      production = {
        project_id      = "mindclade-production-net"
        network         = "projects/mindclade-production-net/global/networks/production-vpc"
        region          = "us-central1"
        router_name     = "production-router"
        nat_name        = "production-nat"
        static_ip_count = 2
      }
    }
  }
  assert {
    condition = (
      length(google_compute_address.nat) == 2 &&
      google_compute_router_nat.nat["production"].nat_ip_allocate_option == "MANUAL_ONLY" &&
      google_compute_router_nat.nat["production"].log_config[0].filter == "ERRORS_ONLY"
    )
    error_message = "Manual NAT must retain protected addresses and error logging."
  }
}

run "reject_auto_nat_with_static_addresses" {
  command = plan
  variables {
    nats = {
      invalid = {
        project_id             = "mindclade-development-net"
        network                = "projects/mindclade-development-net/global/networks/development-vpc"
        region                 = "us-central1"
        router_name            = "development-router"
        nat_name               = "development-nat"
        nat_ip_allocate_option = "AUTO_ONLY"
        static_ip_count        = 2
      }
    }
  }
  expect_failures = [var.nats]
}
