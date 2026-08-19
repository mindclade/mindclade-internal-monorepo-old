# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# Written against the map interface this module actually has.
#
# It used to pass `project_id`, `network_name`, `subnets` and `nat_gateways` as top-level
# variables, which is the shape the module had before 3-networks became one unit covering all
# three environments. Every run failed on "No value for required variable: networks", and the
# suite was red — so `terraform test` was never wired into CI here, and nothing said why.
#
# Resource addresses are keyed accordingly: networks by their map key, subnets and NAT
# gateways by the composite "<network>/<key>" the module builds in locals.

mock_provider "google" {}

run "private_network_contract" {
  command = plan

  variables {
    networks = {
      prod = {
        project_id   = "mindclade-network-prod"
        network_name = "mindclade-prod"

        # `nodes` is the default primary_subnet_key, and the module validates that it exists,
        # is PRIVATE, and carries exactly one `-pods` and one `-services` secondary range —
        # because the GKE units downstream index those outputs by name.
        subnets = {
          nodes = {
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
            subnet_keys = ["nodes"]
          }
        }
      }
    }
  }

  assert {
    condition = (
      google_compute_network.this["prod"].auto_create_subnetworks == false &&
      google_compute_network.this["prod"].delete_default_routes_on_create == true &&
      google_compute_network.this["prod"].deletion_policy == "PREVENT"
    )
    error_message = "The VPC must be custom mode and protected from deletion."
  }

  assert {
    condition = (
      google_compute_subnetwork.this["prod/nodes"].private_ip_google_access == true &&
      google_compute_subnetwork.this["prod/nodes"].deletion_policy == "PREVENT" &&
      google_compute_subnetwork.this["prod/nodes"].log_config[0].metadata == "INCLUDE_ALL_METADATA" &&
      length(google_compute_subnetwork.this["prod/nodes"].secondary_ip_range) == 2
    )
    error_message = "Private subnets must enable Google API access, flow logs, secondary ranges, and deletion guards."
  }

  assert {
    condition = (
      google_compute_route.default_internet["prod"].next_hop_gateway == "default-internet-gateway" &&
      google_compute_route.default_internet["prod"].deletion_policy == "PREVENT" &&
      google_compute_router_nat.this["prod/central"].source_subnetwork_ip_ranges_to_nat == "LIST_OF_SUBNETWORKS" &&
      google_compute_router_nat.this["prod/central"].log_config[0].enable == true &&
      google_compute_router_nat.this["prod/central"].log_config[0].filter == "ERRORS_ONLY"
    )
    error_message = "Internet routing and NAT must be explicit, subnet-scoped, logged, and deletion-protected."
  }
}

# One unit builds every environment, so the interesting case is not a network on its own — it
# is two of them side by side, where a key collision or a shared resource name would surface.
run "environments_do_not_collide" {
  command = plan

  variables {
    networks = {
      development = {
        project_id   = "mindclade-network-dev"
        network_name = "mindclade-dev"
        subnets = {
          nodes = {
            region        = "us-central1"
            ip_cidr_range = "10.60.0.0/20"
            secondary_ip_ranges = {
              dev-pods     = "10.64.0.0/14"
              dev-services = "10.68.0.0/20"
            }
          }
        }
      }
      production = {
        project_id   = "mindclade-network-prod"
        network_name = "mindclade-prod"
        subnets = {
          nodes = {
            region        = "us-central1"
            ip_cidr_range = "10.20.0.0/20"
            secondary_ip_ranges = {
              prod-pods     = "10.24.0.0/14"
              prod-services = "10.28.0.0/20"
            }
          }
        }
      }
    }
  }

  assert {
    condition = (
      length(google_compute_network.this) == 2 &&
      length(google_compute_subnetwork.this) == 2 &&
      google_compute_subnetwork.this["development/nodes"].ip_cidr_range != google_compute_subnetwork.this["production/nodes"].ip_cidr_range
    )
    error_message = "Each environment must get its own network and its own subnet address space."
  }
}

run "network_without_internet_route" {
  command = plan

  variables {
    networks = {
      isolated = {
        project_id                    = "mindclade-network-isolated"
        network_name                  = "mindclade-isolated"
        create_default_internet_route = false
        subnets = {
          nodes = {
            region        = "us-central1"
            ip_cidr_range = "10.40.0.0/20"
            secondary_ip_ranges = {
              isolated-pods     = "10.44.0.0/14"
              isolated-services = "10.48.0.0/20"
            }
          }
        }
      }
    }
  }

  assert {
    condition     = length(google_compute_route.default_internet) == 0 && length(google_compute_router_nat.this) == 0
    error_message = "An isolated network must omit both the default internet route and Cloud NAT."
  }
}

# Public Cloud NAT needs the module-managed default route. Asking for one without the other is
# accepted by the API and produces a gateway that translates nothing.
run "rejects_nat_without_a_default_route" {
  command = plan

  variables {
    networks = {
      prod = {
        project_id                    = "mindclade-network-prod"
        network_name                  = "mindclade-prod"
        create_default_internet_route = false
        subnets = {
          nodes = {
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
            subnet_keys = ["nodes"]
          }
        }
      }
    }
  }

  expect_failures = [google_compute_router_nat.this["prod/central"]]
}

# A gateway claiming a subnet from another region. Terraform resolves the reference — the
# subnet exists — and the API rejects it, so the module checks it where both facts are visible.
run "rejects_cross_region_nat_subnet" {
  command = plan

  variables {
    networks = {
      prod = {
        project_id   = "mindclade-network-prod"
        network_name = "mindclade-prod"
        subnets = {
          nodes = {
            region        = "us-central1"
            ip_cidr_range = "10.20.0.0/20"
            secondary_ip_ranges = {
              prod-pods     = "10.24.0.0/14"
              prod-services = "10.28.0.0/20"
            }
          }
        }
        nat_gateways = {
          east = {
            region      = "us-east1"
            router_name = "mindclade-prod-east-router"
            nat_name    = "mindclade-prod-east-nat"
            subnet_keys = ["nodes"]
          }
        }
      }
    }
  }

  expect_failures = [google_compute_router_nat.this["prod/east"]]
}

run "rejects_invalid_manual_nat_address" {
  command = plan

  variables {
    networks = {
      prod = {
        project_id   = "mindclade-network-prod"
        network_name = "mindclade-prod"
        subnets = {
          nodes = {
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
            region                 = "us-central1"
            router_name            = "mindclade-prod-central-router"
            nat_name               = "mindclade-prod-central-nat"
            subnet_keys            = ["nodes"]
            nat_ip_allocate_option = "AUTO_ONLY"
            # AUTO_ONLY with addresses listed. The API ignores them, so the gateway comes up
            # on addresses nobody chose and the reserved ones sit unused.
            nat_ips = [
              "projects/another-project/regions/us-east1/addresses/nat-address",
            ]
          }
        }
      }
    }
  }

  expect_failures = [var.networks]
}

# Two ACTIVE proxy-only subnets in one region. Rejected by the API with an error naming
# neither subnet, so the module catches it while both are in view.
run "rejects_two_active_proxy_subnets_in_a_region" {
  command = plan

  variables {
    networks = {
      prod = {
        project_id   = "mindclade-network-prod"
        network_name = "mindclade-prod"
        subnets = {
          nodes = {
            region        = "us-central1"
            ip_cidr_range = "10.20.0.0/20"
            secondary_ip_ranges = {
              prod-pods     = "10.24.0.0/14"
              prod-services = "10.28.0.0/20"
            }
          }
          proxy-a = {
            region        = "us-central1"
            ip_cidr_range = "10.21.0.0/24"
            purpose       = "REGIONAL_MANAGED_PROXY"
            role          = "ACTIVE"
            flow_logs     = { enabled = false }
          }
          proxy-b = {
            region        = "us-central1"
            ip_cidr_range = "10.21.1.0/24"
            purpose       = "REGIONAL_MANAGED_PROXY"
            role          = "ACTIVE"
            flow_logs     = { enabled = false }
          }
        }
      }
    }
  }

  expect_failures = [var.networks]
}
