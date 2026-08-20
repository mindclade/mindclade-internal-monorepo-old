# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

run "protected_internal_address" {
  command = plan

  variables {
    addresses = {
      production = {
        project_id = "mindclade-production"
        name       = "production-gateway"
        region     = "us-central1"
        subnetwork = "projects/mindclade-network/regions/us-central1/subnetworks/production-nodes"
        address    = "10.20.0.10"
      }
    }
  }

  assert {
    condition = (
      google_compute_address.this["production"].address_type == "INTERNAL" &&
      google_compute_address.this["production"].deletion_policy == "PREVENT" &&
      google_compute_address.this["production"].purpose == "GCE_ENDPOINT"
    )
    error_message = "The reservation must remain internal and protected."
  }
}
run "reject_public_subnetwork_reference" {
  command = plan

  variables {
    addresses = {
      invalid = {
        project_id = "mindclade-production"
        name       = "invalid-address"
        region     = "us-central1"
        subnetwork = "default"
      }
    }
  }

  expect_failures = [var.addresses]
}

run "reject_non_ipv4_literal" {
  command = plan

  variables {
    addresses = {
      invalid = {
        project_id = "mindclade-production"
        name       = "invalid-address"
        region     = "us-central1"
        subnetwork = "projects/mindclade-network/regions/us-central1/subnetworks/production-nodes"
        address    = "not-an-address"
      }
    }
  }

  expect_failures = [var.addresses]
}
