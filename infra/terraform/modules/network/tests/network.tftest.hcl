# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

run "private_network_contract" {
  command = plan

  variables {
    project_id   = "mindclade-network-prod"
    network_name = "mindclade-prod"
    subnets = {
      prod-central = {
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
        subnet_keys = ["prod-central"]
      }
    }
  }

  assert {
    condition = (
      google_compute_network.this.auto_create_subnetworks == false &&
      google_compute_network.this.delete_default_routes_on_create == true &&
      google_compute_network.this.deletion_policy == "PREVENT"
    )
    error_message = "The VPC must be custom mode and protected from deletion."
  }

  assert {
    condition = (
      google_compute_subnetwork.this["prod-central"].private_ip_google_access == true &&
      google_compute_subnetwork.this["prod-central"].deletion_policy == "PREVENT" &&
      google_compute_subnetwork.this["prod-central"].log_config[0].metadata == "INCLUDE_ALL_METADATA" &&
      length(google_compute_subnetwork.this["prod-central"].secondary_ip_range) == 2
    )
    error_message = "Private subnets must enable Google API access, flow logs, secondary ranges, and deletion guards."
  }

  assert {
    condition = (
      google_compute_route.default_internet[0].next_hop_gateway == "default-internet-gateway" &&
      google_compute_route.default_internet[0].deletion_policy == "PREVENT" &&
      google_compute_router_nat.this["central"].source_subnetwork_ip_ranges_to_nat == "LIST_OF_SUBNETWORKS" &&
      google_compute_router_nat.this["central"].log_config[0].enable == true &&
      google_compute_router_nat.this["central"].log_config[0].filter == "ERRORS_ONLY"
    )
    error_message = "Internet routing and NAT must be explicit, subnet-scoped, logged, and deletion-protected."
  }
}

run "network_without_internet_route" {
  command = plan

  variables {
    project_id                    = "mindclade-network-isolated"
    network_name                  = "mindclade-isolated"
    create_default_internet_route = false
    subnets = {
      isolated-central = {
        region        = "us-central1"
        ip_cidr_range = "10.40.0.0/20"
      }
    }
  }

  assert {
    condition     = length(google_compute_route.default_internet) == 0 && length(google_compute_router_nat.this) == 0
    error_message = "An isolated network must omit both the default internet route and Cloud NAT."
  }
}

run "rejects_cross_region_nat_subnet" {
  command = plan

  variables {
    project_id   = "mindclade-network-prod"
    network_name = "mindclade-prod"
    subnets = {
      prod-central = {
        region        = "us-central1"
        ip_cidr_range = "10.20.0.0/20"
      }
    }
    nat_gateways = {
      east = {
        region      = "us-east1"
        router_name = "mindclade-prod-east-router"
        nat_name    = "mindclade-prod-east-nat"
        subnet_keys = ["prod-central"]
      }
    }
  }

  expect_failures = [google_compute_router_nat.this["east"]]
}

run "rejects_invalid_manual_nat_address" {
  command = plan

  variables {
    project_id   = "mindclade-network-prod"
    network_name = "mindclade-prod"
    subnets = {
      prod-central = {
        region        = "us-central1"
        ip_cidr_range = "10.20.0.0/20"
      }
    }
    nat_gateways = {
      central = {
        region                 = "us-central1"
        router_name            = "mindclade-prod-central-router"
        nat_name               = "mindclade-prod-central-nat"
        subnet_keys            = ["prod-central"]
        nat_ip_allocate_option = "MANUAL_ONLY"
        nat_ips = [
          "projects/another-project/regions/us-east1/addresses/nat-address",
        ]
      }
    }
  }

  expect_failures = [var.nat_gateways]
}

run "rejects_duplicate_regional_router_identity" {
  command = plan

  variables {
    project_id   = "mindclade-network-prod"
    network_name = "mindclade-prod"
    subnets = {
      prod-central-a = {
        region        = "us-central1"
        ip_cidr_range = "10.20.0.0/20"
      }
      prod-central-b = {
        region        = "us-central1"
        ip_cidr_range = "10.21.0.0/20"
      }
    }
    nat_gateways = {
      central-a = {
        region      = "us-central1"
        router_name = "mindclade-prod-central-router"
        nat_name    = "mindclade-prod-central-a-nat"
        subnet_keys = ["prod-central-a"]
      }
      central-b = {
        region      = "us-central1"
        router_name = "mindclade-prod-central-router"
        nat_name    = "mindclade-prod-central-b-nat"
        subnet_keys = ["prod-central-b"]
      }
    }
  }

  expect_failures = [var.nat_gateways]
}
