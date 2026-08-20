# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

output "access_log_bucket" {
  description = "Managed access-log bucket identity and fixed safeguards."
  value = {
    bucket           = module.access_logs.bucket
    deletion_policy  = "PREVENT"
    prevent_destroy  = true
    retention_days   = var.access_log_bucket.retention_days
    writer_principal = local.storage_analytics_writer
  }
}

output "data_buckets" {
  description = "Governed data bucket identities keyed by stable caller key."
  value = {
    for key, bucket in module.data : key => {
      bucket              = bucket.bucket
      data_class          = var.data_buckets[key].data_class
      data_classification = var.data_buckets[key].data_classification
      deletion_policy     = "PREVENT"
      prevent_destroy     = true
    }
  }
}

output "ai_artifact_buckets" {
  description = "Create-only AI artifact bucket identities keyed by stable caller key."
  value = {
    for key, bucket in module.ai_artifacts : key => {
      bucket          = bucket.bucket
      artifact_class  = var.ai_artifact_buckets[key].artifact_class
      create_only     = true
      deletion_policy = "PREVENT"
      prevent_destroy = true
    }
  }
}

output "required_upstream_access_log_writer_grant" {
  description = "Grant required in the separately owned upstream access-log bucket state."
  value       = module.access_logs.required_access_log_writer_grant
}

output "kms_key_names" {
  description = "CMEK names by bucket class for key-IAM and rotation verification."
  value = {
    access_logs  = module.access_logs.kms_key_name
    data         = { for key, bucket in module.data : key => bucket.kms_key_name }
    ai_artifacts = { for key, bucket in module.ai_artifacts : key => bucket.kms_key_name }
  }
}

output "required_kms_grants" {
  description = "Additive grants the KMS-owning state must apply for the Cloud Storage service agent."
  value = merge(
    {
      access_logs = {
        crypto_key = var.access_log_bucket.kms_key_name
        member     = "serviceAccount:${var.storage_service_agent_email}"
        role       = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
      }
    },
    {
      for key, bucket in var.data_buckets : "data/${key}" => {
        crypto_key = bucket.kms_key_name
        member     = "serviceAccount:${var.storage_service_agent_email}"
        role       = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
      }
    },
    {
      for key, bucket in var.ai_artifact_buckets : "ai-artifact/${key}" => {
        crypto_key = bucket.kms_key_name
        member     = "serviceAccount:${var.storage_service_agent_email}"
        role       = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
      }
    },
  )
}
