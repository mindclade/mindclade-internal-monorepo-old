# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

resource "google_gke_backup_backup_plan" "this" {
  project         = var.project_id
  name            = var.gke_backup.plan_name
  location        = var.gke_backup.location
  cluster         = var.gke_backup.cluster
  description     = "Deletion-recovery backup governed by infrastructure-live."
  labels          = merge(var.labels, { managed-by = "terraform" })
  deletion_policy = "PREVENT"

  backup_schedule { cron_schedule = var.gke_backup.cron_schedule }
  retention_policy {
    backup_retain_days      = var.gke_backup.retention.backup_retain_days
    backup_delete_lock_days = var.gke_backup.retention.backup_delete_lock_days
  }
  backup_config {
    all_namespaces      = true
    include_volume_data = var.gke_backup.include_volume_data
    include_secrets     = var.gke_backup.include_secrets
    encryption_key { gcp_kms_encryption_key = var.gke_backup.encryption_key }
  }

  lifecycle { prevent_destroy = true }
}

resource "google_storage_bucket" "replica" {
  for_each = var.bucket_replication

  project                     = var.project_id
  name                        = each.value.destination_bucket
  location                    = each.value.destination_region
  force_destroy               = false
  deletion_policy             = "PREVENT"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  labels                      = merge(var.labels, { managed-by = "terraform", purpose = "dr-replica" })

  encryption { default_kms_key_name = each.value.kms_key_name }
  versioning { enabled = true }
  soft_delete_policy { retention_duration_seconds = 2592000 }
  retention_policy {
    retention_period = tostring(each.value.retention_days * 86400)
    is_locked        = false
  }
  lifecycle {
    prevent_destroy = true
  }
}

resource "google_storage_transfer_job" "replica" {
  for_each = var.bucket_replication

  project         = var.project_id
  description     = "Deletion-independent replication: ${each.value.source_bucket} to ${each.value.destination_bucket}"
  status          = "ENABLED"
  deletion_policy = "PREVENT"

  transfer_spec {
    gcs_data_source { bucket_name = each.value.source_bucket }
    gcs_data_sink { bucket_name = google_storage_bucket.replica[each.key].name }
    transfer_options {
      delete_objects_unique_in_sink              = each.value.delete_objects_unique_in_sink
      delete_objects_from_source_after_transfer  = each.value.delete_objects_from_source_after_transfer
      overwrite_objects_already_existing_in_sink = each.value.overwrite_objects_already_existing_in_sink
    }
  }
  schedule {
    schedule_start_date {
      year  = 2026
      month = 8
      day   = 20
    }
    start_time_of_day {
      hours   = split(" ", each.value.schedule)[1] == "*" ? 0 : tonumber(split(" ", each.value.schedule)[1])
      minutes = tonumber(split(" ", each.value.schedule)[0])
      seconds = 0
      nanos   = 0
    }
    repeat_interval = split(" ", each.value.schedule)[1] == "*" ? "3600s" : "86400s"
  }
}
