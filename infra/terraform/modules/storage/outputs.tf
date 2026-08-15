# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "bucket" {
  description = "Bucket resource"
  value = {
    id        = google_storage_bucket.this.id
    name      = google_storage_bucket.this.name
    self_link = google_storage_bucket.this.self_link
    url       = google_storage_bucket.this.url
  }
}

output "kms_key_name" {
  description = "Configured default CryptoKey, if any"
  value       = var.kms_key_name
}

output "required_access_log_writer_grant" {
  description = "Additive IAM grant the separately owned access-log bucket must implement"
  value = {
    bucket = var.access_log_bucket
    member = "group:cloud-storage-analytics@google.com"
    role   = "roles/storage.objectCreator"
  }
}
