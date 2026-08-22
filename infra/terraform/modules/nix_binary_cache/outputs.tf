# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "bucket" {
  description = "Hardened Nix binary-cache bucket resource"
  value       = module.cache.bucket
}

output "substituter_uri" {
  description = "Reserved client substituter URI; null because a raw private GCS bucket is storage, not an authenticated Nix substituter"
  value       = null
}

output "storage_https_uri" {
  description = "Private Cloud Storage HTTPS endpoint for backend integration; this is not a Nix substituter"
  value       = "https://storage.googleapis.com/${var.bucket_name}"
}

output "gs_uri" {
  description = "Cloud Storage URI used by authenticated publication tooling"
  value       = "gs://${var.bucket_name}"
}

output "kms_key_name" {
  description = "Default CMEK CryptoKey"
  value       = module.cache.kms_key_name
}

output "immutable_policy" {
  description = "Reviewable immutable-publication durability and deletion contract"
  value = {
    data_lifecycle              = "immutable"
    data_classification         = var.data_classification
    storage_class               = "STANDARD"
    versioning_enabled          = true
    create_only_publishers      = true
    object_delete_rules         = 0
    soft_delete_retention_days  = var.soft_delete_retention_days
    retention_period_seconds    = var.retention_period_seconds
    retention_policy_locked     = var.lock_retention_policy
    force_destroy               = false
    deletion_policy             = "PREVENT"
    uniform_bucket_level_access = true
    public_access_prevention    = "enforced"
  }
}

output "iam_contract" {
  description = "Additive non-public bucket IAM contract; publishers are also effective readers"
  value = {
    readers = {
      role    = "roles/storage.objectViewer"
      members = local.effective_reader_members
    }
    publishers = {
      role    = "roles/storage.objectCreator"
      members = var.writer_members
    }
    object_admin_members = toset([])
  }
}

output "client_activation_contract" {
  description = "Fail-closed client boundary for the raw private storage backend"
  value = {
    enabled                     = false
    substituter_uri             = null
    trusted_public_key          = null
    authentication              = "unqualified"
    backend_protocol            = "gcs-json-xml-storage-only"
    reason                      = "raw-private-gcs-is-not-a-nix-substituter"
    signing_key_in_client_scope = false
  }
}

output "required_access_log_writer_grant" {
  description = "Additive grant the separately owned access-log bucket must implement"
  value       = module.cache.required_access_log_writer_grant
}
