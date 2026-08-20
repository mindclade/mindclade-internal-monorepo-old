# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

output "sink_ids" {
  description = "Aggregated sink resource IDs keyed by sink name, independent of parent type."
  value = merge(
    { for name, sink in google_logging_organization_sink.logging : name => sink.id },
    { for name, sink in google_logging_organization_sink.storage : name => sink.id },
    { for name, sink in google_logging_folder_sink.logging : name => sink.id },
    { for name, sink in google_logging_folder_sink.storage : name => sink.id },
  )
}

output "writer_identities" {
  description = "Unique service account used by each sink, keyed by sink name."
  value = merge(
    local.logging_writer_identities,
    local.storage_writer_identities,
  )
}

output "log_bucket_ids" {
  description = "Cloud Logging bucket IDs keyed by sink name."
  value       = { for name, bucket in google_logging_project_bucket_config.this : name => bucket.id }
}

output "storage_bucket_names" {
  description = "GCS archive bucket names keyed by sink name."
  value       = { for name, bucket in google_storage_bucket.this : name => bucket.name }
}

output "storage_kms_key_names" {
  description = "Configured GCS archive CMEK names, excluding archives that use Google-managed encryption."
  value = {
    for name, sink in local.storage_sinks : name => sink.bucket.encryption_key
    if sink.bucket.encryption_key != null
  }
}

output "required_access_log_writer_grants" {
  description = "Additive grants required on separately governed Cloud Storage access-log buckets."
  value = {
    for name, sink in local.storage_sinks : name => {
      bucket = sink.bucket.access_log_bucket_name
      member = "group:cloud-storage-analytics@google.com"
      role   = "roles/storage.objectCreator"
      prefix = sink.bucket.access_log_object_prefix
    }
  }
}

output "analytics_enabled_buckets" {
  description = "Cloud Logging destinations configured with Log Analytics."
  value       = sort([for name, sink in local.logging_sinks : name if sink.enable_analytics])
}

output "default_bucket_scope" {
  description = "The only _Default bucket this module can manage; null means it is left unmanaged."
  value = var.default_sink_retention_days == 0 ? null : {
    project        = var.project_id
    location       = "global"
    retention_days = var.default_sink_retention_days
  }
}
