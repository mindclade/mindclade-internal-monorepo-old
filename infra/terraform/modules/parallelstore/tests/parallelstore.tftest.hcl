# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}
run "scratch_capacity_is_bounded" {
  command = plan
  variables {
    project_id = "mindclade-development-research"
    parallelstore = {
      scratch = {
        name              = "mindclade-development-scratch", location = "us-central1-a", capacity_gib = 12000,
        deployment_type   = "SCRATCH", network = "projects/mock/global/networks/development",
        reserved_ip_range = "development-service-networking", gcs_import = { source = "gs://mindclade-development-lake-features" }
      }
    }
  }
  assert {
    condition     = google_parallelstore_instance.this["scratch"].deletion_policy == "PREVENT"
    error_message = "Parallelstore deletion must remain protected."
  }
}
