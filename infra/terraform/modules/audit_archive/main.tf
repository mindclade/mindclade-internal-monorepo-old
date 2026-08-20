# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# Central Cloud Audit Logs composition. There are intentionally no exclusions or
# caller-supplied narrowing filters in this immutable evidence path.

locals {
  audit_filter = <<-EOT
    log_id("cloudaudit.googleapis.com/activity") OR
    log_id("cloudaudit.googleapis.com/system_event") OR
    log_id("cloudaudit.googleapis.com/policy") OR
    log_id("cloudaudit.googleapis.com/data_access")
  EOT

  baseline_labels = {
    "data-classification" = "restricted"
    environment           = var.environment
    "managed-by"          = "terraform"
    owner                 = var.owner
    purpose               = "audit-archive"
  }
}

module "archive" {
  source = "../log_sink"

  parent                      = var.parent
  project_id                  = var.project_id
  include_children            = true
  default_sink_retention_days = var.destination_project_default_retention_days
  retention_lock_confirmation = var.retention_lock_confirmation
  labels                      = merge(var.labels, local.baseline_labels)

  sinks = {
    (var.sink_name) = {
      description = "Immutable central Cloud Audit Logs archive."
      destination = "storage"
      filter      = local.audit_filter
      exclusions  = []

      bucket = {
        name                       = var.bucket_name
        location                   = var.location
        access_log_bucket_name     = var.access_log_bucket_name
        access_log_object_prefix   = var.access_log_object_prefix
        encryption_key             = var.kms_key_name
        retention_days             = var.retention_days
        lock_retention_policy      = true
        soft_delete_retention_days = var.soft_delete_retention_days
        lifecycle_rules = [
          {
            age           = 30
            action        = "SetStorageClass"
            storage_class = "NEARLINE"
          },
          {
            age           = 90
            action        = "SetStorageClass"
            storage_class = "COLDLINE"
          },
          {
            age           = 365
            action        = "SetStorageClass"
            storage_class = "ARCHIVE"
          },
        ]
      }
    }
  }
}
