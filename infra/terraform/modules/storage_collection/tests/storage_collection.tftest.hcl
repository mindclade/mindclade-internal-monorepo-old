# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

mock_provider "google" {}
run "locked_holdout_is_resource_scoped" {
  command = plan
  variables {
    project_id                  = "mindclade-development-data"
    location                    = "us-central1"
    encryption_key              = "projects/mindclade-seed/locations/us-central1/keyRings/development/cryptoKeys/storage"
    retention_lock_confirmation = "LOCKING A CLOUD STORAGE RETENTION POLICY IS IRREVERSIBLE"
    buckets                     = { holdout = { name = "mindclade-development-holdout", retention_days = 3650 } }
    deny_policies = {
      holdout-no-training-read = {
        display_name = "Training may not read holdout"
        rules = [{
          denied_principals  = ["principal://iam.googleapis.com/projects/-/serviceAccounts/training@mindclade-development-data.iam.gserviceaccount.com"]
          denied_permissions = ["storage.googleapis.com/objects.get"]
        }]
      }
    }
  }
  assert {
    condition     = google_storage_bucket.this["holdout"].retention_policy[0].is_locked
    error_message = "Holdout retention must be locked."
  }
}
