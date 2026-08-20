# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "key_ring_id" {
  description = "Fully qualified Cloud KMS key-ring resource ID"
  value       = google_kms_key_ring.this.id
}

output "key_ring_name" {
  description = "Cloud KMS key-ring name"
  value       = google_kms_key_ring.this.name
}

output "crypto_key_ids" {
  description = "Fully qualified CryptoKey resource IDs keyed by key name"
  value       = { for name, key in google_kms_crypto_key.this : name => key.id }
}

output "crypto_key_names" {
  description = "CryptoKey names keyed by the stable input name"
  value       = { for name, key in google_kms_crypto_key.this : name => key.name }
}

output "signing_key_ids" {
  description = "Asymmetric signing CryptoKey ids by name. The BFF signs by calling KMS with one of these; the private half never leaves."
  value       = { for name, key in google_kms_crypto_key.signing : name => key.id }
}

output "signing_key_names" {
  description = "Asymmetric signing CryptoKey short names."
  value       = { for name, key in google_kms_crypto_key.signing : name => key.name }
}
