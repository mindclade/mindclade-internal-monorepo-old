# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "policy_id" {
  description = "Binary Authorization policy resource id."
  value       = google_binary_authorization_policy.this.id
}

output "project_id" {
  description = "Project that owns the policy, attestors, notes, and attestation occurrences; consumers must not reconstruct it from resource names."
  value       = var.project_id
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

output "signer_grants" {
  description = "Attestor-scoped note-attacher and KMS signer grants created for attestation producers."
  value = {
    for key, pair in local.signer_pairs : key => {
      attestor = pair.attestor
      member   = pair.member
      roles = [
        "roles/containeranalysis.notes.attacher",
        "roles/cloudkms.signerVerifier",
      ]
    }
  }
}

output "required_occurrence_permissions" {
  description = "Exact project-level occurrence permissions the live caller must grant through one custom role; update and delete are prohibited."
  value       = local.required_occurrence_permissions
}

output "verifier_grants" {
  description = "Attestor verifier grants created for Binary Authorization service agents."
  value       = { for key, pair in local.verifier_pairs : key => pair }
}

output "enforcement_mode" {
  description = "The default rule's enforcement mode, surfaced so a caller can assert that production is not silently in dry-run."
  value       = google_binary_authorization_policy.this.default_admission_rule[0].enforcement_mode
}

output "exempt_image_patterns" {
  description = "Image patterns admitted without attestation. Every entry here is a hole by design; the list is an output so it appears in a plan diff."
  value       = sort(var.exempt_images)
}
