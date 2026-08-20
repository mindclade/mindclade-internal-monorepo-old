# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# This is a composition module. The sibling storage module remains the only authority for
# google_storage_bucket and bucket IAM resources.

locals {
  storage_analytics_writer = "group:cloud-storage-analytics@google.com"

  # Keep the child module evaluable when the wrapper's cross-variable validation
  # rejects a self-logging cycle. The root validation still fails the plan; this
  # sentinel prevents a second, less actionable child-resource diagnostic.
  validated_upstream_access_log_bucket = (
    var.upstream_access_log_bucket_name == var.access_log_bucket.name
    ? "invalid-object-storage-composition"
    : var.upstream_access_log_bucket_name
  )
}

module "access_logs" {
  source = "../storage"

  project_id                 = var.project_id
  name                       = var.access_log_bucket.name
  location                   = var.access_log_bucket.location
  storage_class              = var.access_log_bucket.storage_class
  environment                = var.environment
  owner                      = var.owner
  data_classification        = "restricted"
  labels                     = merge(var.labels, var.access_log_bucket.labels, { "bucket-class" = "access-log" })
  kms_key_name               = var.access_log_bucket.kms_key_name
  access_log_bucket          = local.validated_upstream_access_log_bucket
  access_log_object_prefix   = "object-storage-access-log-bucket/"
  versioning_enabled         = true
  soft_delete_retention_days = var.access_log_bucket.soft_delete_retention_days
  retention_period_seconds   = var.access_log_bucket.retention_days * 86400
  lock_retention_policy      = false
  object_viewers             = var.access_log_bucket.viewers
  object_creators            = [local.storage_analytics_writer]
  object_admins              = []

  lifecycle_rules = [
    {
      action        = "AbortIncompleteMultipartUpload"
      age_days      = 7
      storage_class = null
      with_state    = null
    },
    {
      action        = "SetStorageClass"
      age_days      = 90
      storage_class = "COLDLINE"
      with_state    = "LIVE"
    },
    {
      action        = "SetStorageClass"
      age_days      = 365
      storage_class = "ARCHIVE"
      with_state    = "LIVE"
    },
  ]
}

module "data" {
  for_each = var.data_buckets
  source   = "../storage"

  project_id                 = var.project_id
  name                       = each.value.name
  location                   = each.value.location
  storage_class              = each.value.storage_class
  environment                = var.environment
  owner                      = var.owner
  data_classification        = each.value.data_classification
  labels                     = merge(var.labels, each.value.labels, { "bucket-class" = "data", "data-class" = each.value.data_class })
  kms_key_name               = each.value.kms_key_name
  access_log_bucket          = module.access_logs.bucket.name
  access_log_object_prefix   = "data/${each.key}/"
  versioning_enabled         = true
  soft_delete_retention_days = each.value.soft_delete_retention_days
  retention_period_seconds   = each.value.retention_period_seconds
  lock_retention_policy      = false
  lifecycle_rules            = each.value.lifecycle_rules
  object_viewers             = each.value.readers
  object_creators            = each.value.writers
  object_admins              = each.value.admins
}

module "ai_artifacts" {
  for_each = var.ai_artifact_buckets
  source   = "../storage"

  project_id                 = var.project_id
  name                       = each.value.name
  location                   = each.value.location
  storage_class              = each.value.storage_class
  environment                = var.environment
  owner                      = var.owner
  data_classification        = "restricted"
  labels                     = merge(var.labels, each.value.labels, { "artifact-class" = each.value.artifact_class, "bucket-class" = "ai-artifact" })
  kms_key_name               = each.value.kms_key_name
  access_log_bucket          = module.access_logs.bucket.name
  access_log_object_prefix   = "ai-artifacts/${each.key}/"
  versioning_enabled         = true
  create_only_workload       = true
  soft_delete_retention_days = each.value.soft_delete_retention_days
  retention_period_seconds   = each.value.retention_period_seconds
  lock_retention_policy      = false
  object_creators            = each.value.publishers
  object_viewers             = setunion(each.value.publishers, each.value.readers)
  object_admins              = []

  lifecycle_rules = [{
    action        = "AbortIncompleteMultipartUpload"
    age_days      = 7
    storage_class = null
    with_state    = null
  }]
}
