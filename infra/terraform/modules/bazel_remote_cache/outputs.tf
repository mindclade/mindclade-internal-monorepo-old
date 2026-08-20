# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "bucket" {
  description = "Hardened Bazel remote-cache bucket resource"
  value       = module.cache.bucket
}

output "gs_uri" {
  description = "Cloud Storage URI for cache backend configuration"
  value       = "gs://${var.bucket_name}"
}

output "https_uri" {
  description = "Authenticated HTTPS endpoint prefix; this is not a public URL"
  value       = "https://storage.googleapis.com/${var.bucket_name}"
}

output "kms_key_name" {
  description = "Default CMEK CryptoKey"
  value       = module.cache.kms_key_name
}

output "cache_policy" {
  description = "Reviewable rebuildable-cache durability and deletion contract"
  value = {
    data_lifecycle              = "rebuildable"
    data_classification         = var.data_classification
    storage_class               = "STANDARD"
    versioning_enabled          = true
    cache_ttl_days              = var.cache_ttl_days
    noncurrent_version_ttl_days = var.noncurrent_version_ttl_days
    soft_delete_retention_days  = var.soft_delete_retention_days
    retention_period_seconds    = var.retention_period_seconds
    retention_policy_locked     = false
    force_destroy               = false
    deletion_policy             = "PREVENT"
    uniform_bucket_level_access = true
    public_access_prevention    = "enforced"
  }
}

output "iam_contract" {
  description = "Additive non-public bucket IAM contract; cache writers are also effective readers"
  value = {
    readers = {
      role    = "roles/storage.objectViewer"
      members = local.effective_reader_members
    }
    writers = {
      role    = "roles/storage.objectCreator"
      members = var.writer_members
    }
    object_admin_members = toset([])
  }
}

output "required_access_log_writer_grant" {
  description = "Additive grant the separately owned access-log bucket must implement"
  value       = module.cache.required_access_log_writer_grant
}
