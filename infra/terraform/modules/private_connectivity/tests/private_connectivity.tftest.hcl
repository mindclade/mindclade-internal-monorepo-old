# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

mock_provider "google" {}
run "isolated_service_networking_ranges" {
  command = plan
  variables {
    service_networking = {
      development = { project_id = "mindclade-development-net", network = "projects/mock/global/networks/development", allocated_range = "10.16.240.0/24" }
      production  = { project_id = "mindclade-production-net", network = "projects/mock/global/networks/production", allocated_range = "10.48.240.0/24" }
    }
  }
  assert {
    condition     = google_compute_global_address.service_networking["development"].address != google_compute_global_address.service_networking["production"].address
    error_message = "Environment PSA allocations must remain isolated."
  }
}
