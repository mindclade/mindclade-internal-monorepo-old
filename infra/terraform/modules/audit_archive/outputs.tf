# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

output "sink_id" {
  description = "Aggregated organization or folder sink ID."
  value       = module.archive.sink_ids[var.sink_name]
}

output "writer_identity" {
  description = "Unique sink writer identity granted append-only access to the archive."
  value       = module.archive.writer_identities[var.sink_name]
}

output "bucket_name" {
  description = "Central immutable audit archive bucket name."
  value       = module.archive.storage_bucket_names[var.sink_name]
}

output "audit_contract" {
  description = "Reviewable immutable audit controls and coverage intent."
  value = {
    parent                     = var.parent
    include_children           = true
    filter                     = local.audit_filter
    exclusions                 = []
    retention_days             = var.retention_days
    retention_locked           = true
    soft_delete_retention_days = var.soft_delete_retention_days
    cmek_key_name              = var.kms_key_name
    force_destroy              = false
    deletion_policy            = "PREVENT"
    terraform_prevent_destroy  = true
  }
}

output "required_kms_grant" {
  description = "Additive grant the archive CryptoKey-owning state must apply."
  value = {
    crypto_key = var.kms_key_name
    member     = "serviceAccount:${var.storage_service_agent_email}"
    role       = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  }
}
