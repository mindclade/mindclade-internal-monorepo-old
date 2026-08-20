# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  effective_reader_members = setunion(var.reader_members, var.writer_members)

  lifecycle_rules = [
    {
      action                     = "AbortIncompleteMultipartUpload"
      storage_class              = null
      age_days                   = 1
      days_since_noncurrent_time = null
      matches_prefix             = null
      matches_suffix             = null
      num_newer_versions         = null
      with_state                 = null
    },
    {
      action                     = "Delete"
      storage_class              = null
      age_days                   = var.cache_ttl_days
      days_since_noncurrent_time = null
      matches_prefix             = null
      matches_suffix             = null
      num_newer_versions         = null
      with_state                 = "LIVE"
    },
    {
      action                     = "Delete"
      storage_class              = null
      age_days                   = null
      days_since_noncurrent_time = var.noncurrent_version_ttl_days
      matches_prefix             = null
      matches_suffix             = null
      num_newer_versions         = 1
      with_state                 = "ARCHIVED"
    },
  ]
}

module "cache" {
  source = "../storage"

  project_id          = var.project_id
  name                = var.bucket_name
  location            = var.location
  storage_class       = "STANDARD"
  environment         = var.environment
  owner               = var.owner
  data_classification = var.data_classification
  labels = merge(var.labels, {
    "artifact-kind"  = "bazel-remote-cache"
    "data-lifecycle" = "rebuildable"
  })

  kms_key_name                = var.kms_key_name
  access_log_bucket           = var.access_log_bucket
  access_log_object_prefix    = var.access_log_object_prefix
  versioning_enabled          = true
  create_only_workload        = false
  soft_delete_retention_days  = var.soft_delete_retention_days
  retention_period_seconds    = var.retention_period_seconds
  lock_retention_policy       = false
  retention_lock_confirmation = null
  lifecycle_rules             = local.lifecycle_rules
  object_viewers              = local.effective_reader_members
  object_creators             = var.writer_members
  object_admins               = []
}
