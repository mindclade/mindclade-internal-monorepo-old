# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

output "image" {
  description = "Immutable Compute Image identity"
  value = {
    id        = google_compute_image.this.id
    name      = google_compute_image.this.name
    self_link = google_compute_image.this.self_link
  }
}

output "source_contract" {
  description = "Create-only artifact identity that must match release provenance"
  value = {
    bucket_name           = var.source_bucket_name
    uri                   = var.source_uri
    object_generation     = var.source_object_generation
    sha256                = var.source_sha256
    image_contract_sha256 = var.image_contract_sha256
    content_addressed     = true
    mutable_family        = false
  }
}

output "encryption_contract" {
  description = "CMEK identity and external grant required by the image import"
  value = {
    kms_key_name              = var.kms_key_name
    compute_service_account   = var.compute_service_account_email
    required_role             = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
    provider_deletion_policy  = "PREVENT"
    terraform_prevent_destroy = true
  }
}
