# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# Binary Authorization: the point where a signature becomes a precondition for a pod running.
#
# Signing an image and producing an SBOM changes nothing on its own — a cluster that will run
# any image gains nothing from a signature existing somewhere. This module is the other half.
#
# The failure mode worth knowing: REQUIRE_ATTESTATION with an empty attestor list admits
# every image while reading as the strictest setting available. Both the default rule and the
# per-namespace rules reject that combination in variable validation rather than discovering
# it during an incident.

locals {
  # Every attestor named by a rule must exist, or the policy references an attestor Google
  # cannot resolve and admission fails closed on production and silently on dry-run.
  referenced_attestors = distinct(concat(
    var.default_admission_rule.require_attestations_by,
    flatten([for ns, r in var.cluster_admission_rules : r.require_attestations_by]),
  ))

  unknown_attestors = setsubtract(local.referenced_attestors, keys(var.attestors))

  # Signers named for an attestor that does not exist is a grant that silently goes nowhere.
  unknown_signers = setsubtract(keys(var.attestor_signers), keys(var.attestors))

  # Filtered to attestors that exist. Not to tolerate a typo — the precondition below still
  # fails the apply — but so that the failure is that precondition's message rather than an
  # "Invalid index" raised from the IAM resource before any guard gets to run.
  signer_pairs = merge([
    for attestor, members in var.attestor_signers : {
      for m in members : "${attestor}:${m}" => { attestor = attestor, member = m }
    } if contains(keys(var.attestors), attestor)
  ]...)
}

# ---------------------------------------------------------------------------------------
# Signing keys
# ---------------------------------------------------------------------------------------
# Asymmetric SIGN keys, in the ring 1-org/kms reserves for them. The private half never
# leaves KMS: the build pipeline signs by calling KMS, so a compromised pipeline can produce
# attestations while it is compromised but cannot take the key with it.

resource "google_kms_crypto_key" "attestor" {
  for_each = var.attestors

  name     = "attestor-${each.key}"
  key_ring = var.attestor_key_ring
  purpose  = "ASYMMETRIC_SIGN"
  labels   = var.labels

  version_template {
    algorithm        = each.value.kms_key_algorithm
    protection_level = each.value.kms_protection
  }

  # Destroying an attestor key invalidates every attestation ever made with it, including
  # those on images currently running.
  lifecycle {
    prevent_destroy = true
  }
}

data "google_kms_crypto_key_version" "attestor" {
  for_each = var.attestors

  crypto_key = google_kms_crypto_key.attestor[each.key].id
}

# ---------------------------------------------------------------------------------------
# Attestors
# ---------------------------------------------------------------------------------------

resource "google_container_analysis_note" "attestor" {
  for_each = var.attestors

  project = var.project_id
  name    = "attestor-${each.key}"

  attestation_authority {
    hint {
      human_readable_name = each.value.description
    }
  }
}

resource "google_binary_authorization_attestor" "this" {
  for_each = var.attestors

  project     = var.project_id
  name        = each.key
  description = each.value.description

  attestation_authority_note {
    note_reference = google_container_analysis_note.attestor[each.key].name

    public_keys {
      id = data.google_kms_crypto_key_version.attestor[each.key].id

      pkix_public_key {
        public_key_pem      = data.google_kms_crypto_key_version.attestor[each.key].public_key[0].pem
        signature_algorithm = data.google_kms_crypto_key_version.attestor[each.key].public_key[0].algorithm
      }
    }
  }
}

# Who may sign. Deliberately not the same principal that may deploy: an identity that can
# both attest and deploy can approve its own artefacts, which is the control removed.
resource "google_binary_authorization_attestor_iam_member" "signer" {
  for_each = local.signer_pairs

  project  = var.project_id
  attestor = google_binary_authorization_attestor.this[each.value.attestor].name
  role     = "roles/binaryauthorization.attestorsVerifier"
  member   = each.value.member
}

# ---------------------------------------------------------------------------------------
# The policy
# ---------------------------------------------------------------------------------------

resource "google_binary_authorization_policy" "this" {
  project = var.project_id

  global_policy_evaluation_mode = var.global_policy_evaluation_mode

  default_admission_rule {
    evaluation_mode  = var.default_admission_rule.evaluation_mode
    enforcement_mode = var.default_admission_rule.enforcement_mode

    require_attestations_by = [
      for name in var.default_admission_rule.require_attestations_by :
      google_binary_authorization_attestor.this[name].name
    ]
  }

  dynamic "admission_whitelist_patterns" {
    for_each = var.exempt_images

    content {
      name_pattern = admission_whitelist_patterns.value
    }
  }

  dynamic "cluster_admission_rules" {
    for_each = var.cluster == null ? {} : var.cluster_admission_rules

    content {
      # Google matches this literally. A namespace rule is keyed by
      # <location>.<cluster>.<namespace>, and a rule whose cluster string is wrong applies to
      # nothing while still appearing in the policy.
      cluster          = "${var.cluster}.${cluster_admission_rules.key}"
      evaluation_mode  = cluster_admission_rules.value.evaluation_mode
      enforcement_mode = cluster_admission_rules.value.enforcement_mode

      require_attestations_by = [
        for name in cluster_admission_rules.value.require_attestations_by :
        google_binary_authorization_attestor.this[name].name
      ]
    }
  }

  lifecycle {
    precondition {
      condition     = length(local.unknown_attestors) == 0
      error_message = "Rules reference attestors that are not declared: ${join(", ", local.unknown_attestors)}."
    }

    precondition {
      condition     = length(local.unknown_signers) == 0
      error_message = "attestor_signers names attestors that do not exist: ${join(", ", local.unknown_signers)}."
    }

    precondition {
      condition     = var.cluster != null || length(var.cluster_admission_rules) == 0
      error_message = "cluster_admission_rules were given but cluster is null, so none of them would apply."
    }
  }
}
