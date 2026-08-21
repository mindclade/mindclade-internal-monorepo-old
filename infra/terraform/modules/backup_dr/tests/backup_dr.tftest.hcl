# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

mock_provider "google" {
  mock_data "google_project" { defaults = { number = "123456789012" } }
}
run "deletion_independent_replica" {
  command = plan
  variables {
    project_id = "mindclade-production-platform"
    gke_backup = {
      plan_name           = "mindclade-production-backup", cluster = "projects/mindclade-production-platform/locations/europe-west4/clusters/mindclade-production",
      location            = "us-central1", cron_schedule = "0 * * * *", all_namespaces = true, excluded_namespaces = ["kube-system"],
      include_volume_data = true, include_secrets = true,
      encryption_key      = "projects/mindclade-seed/locations/us-central1/keyRings/production/cryptoKeys/storage",
      retention           = { backup_retain_days = 90, backup_delete_lock_days = 30 }
    }
    bucket_replication = {
      checkpoints = {
        source_bucket                             = "mindclade-production-checkpoints", destination_bucket = "mindclade-production-checkpoints-replica",
        destination_region                        = "us-central1", delete_objects_unique_in_sink = false,
        delete_objects_from_source_after_transfer = false, overwrite_objects_already_existing_in_sink = true,
        schedule                                  = "0 4 * * *", retention_days = 90
      }
    }
  }
  assert {
    condition     = !google_storage_transfer_job.replica["checkpoints"].transfer_spec[0].transfer_options[0].delete_objects_unique_in_sink
    error_message = "DR replication must never propagate source deletion."
  }
}
