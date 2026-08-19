# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "policy_id" {
  description = "Binary Authorization policy resource id."
  value       = google_binary_authorization_policy.this.id
}

output "attestor_ids" {
  description = "Attestor resource ids keyed by short name."
  value       = { for name, a in google_binary_authorization_attestor.this : name => a.id }
}

output "attestor_names" {
  description = "Attestor names keyed by short name. These are the values a signing pipeline passes to `gcloud container binauthz attestations sign-and-create --attestor`."
  value       = { for name, a in google_binary_authorization_attestor.this : name => a.name }
}

output "attestor_key_ids" {
  description = "KMS signing key ids keyed by attestor. The private half never leaves KMS; a pipeline signs by calling it."
  value       = { for name, k in google_kms_crypto_key.attestor : name => k.id }
}

output "attestor_key_versions" {
  description = "Signing key version ids keyed by attestor, which is what an attestation records as its public key id."
  value       = { for name, v in data.google_kms_crypto_key_version.attestor : name => v.id }
}

output "enforcement_mode" {
  description = "The default rule's enforcement mode, surfaced so a caller can assert that production is not silently in dry-run."
  value       = google_binary_authorization_policy.this.default_admission_rule[0].enforcement_mode
}

output "exempt_image_patterns" {
  description = "Image patterns admitted without attestation. Every entry here is a hole by design; the list is an output so it appears in a plan diff."
  value       = sort(var.exempt_images)
}
