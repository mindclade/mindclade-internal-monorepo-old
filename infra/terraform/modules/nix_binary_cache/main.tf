# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  effective_reader_members = setunion(var.reader_members, var.writer_members)

  lifecycle_rules = [{
    action                     = "AbortIncompleteMultipartUpload"
    storage_class              = null
    age_days                   = 7
    days_since_noncurrent_time = null
    matches_prefix             = null
    matches_suffix             = null
    num_newer_versions         = null
    with_state                 = null
  }]
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
    "artifact-kind"  = "nix-binary-cache"
    "data-lifecycle" = "immutable"
  })

  kms_key_name                = var.kms_key_name
  access_log_bucket           = var.access_log_bucket
  access_log_object_prefix    = var.access_log_object_prefix
  versioning_enabled          = true
  create_only_workload        = true
  soft_delete_retention_days  = var.soft_delete_retention_days
  retention_period_seconds    = var.retention_period_seconds
  lock_retention_policy       = var.lock_retention_policy
  retention_lock_confirmation = var.retention_lock_confirmation
  lifecycle_rules             = local.lifecycle_rules
  object_viewers              = local.effective_reader_members
  object_creators             = var.writer_members
  object_admins               = []
}
