# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

resource "google_kms_key_ring" "this" {
  project  = var.project_id
  location = var.location
  name     = var.key_ring_name

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_kms_crypto_key" "this" {
  #checkov:skip=CKV_GCP_43:rotation_period is caller-selectable but variables.tf constrains every symmetric key to 1-90 days and mocked tests assert the 90-day default.
  for_each = var.keys

  name                          = each.key
  key_ring                      = google_kms_key_ring.this.id
  purpose                       = "ENCRYPT_DECRYPT"
  rotation_period               = "${each.value.rotation_period_seconds}s"
  destroy_scheduled_duration    = "${each.value.destroy_scheduled_duration_seconds}s"
  skip_initial_version_creation = false
  deletion_policy               = "PREVENT"
  labels = merge(
    var.labels,
    each.value.labels,
    { managed-by = "terraform" },
  )

  version_template {
    algorithm        = "GOOGLE_SYMMETRIC_ENCRYPTION"
    protection_level = each.value.protection_level
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = length(merge(var.labels, each.value.labels, { managed-by = "terraform" })) <= 64
      error_message = "Merged global, key-specific, and managed-by labels must not exceed the Cloud KMS limit of 64."
    }
  }
}

# Asymmetric signing keys. The private half never leaves Cloud KMS.
#
# Note what is ABSENT compared with the symmetric keys above: no `rotation_period`. Cloud KMS
# rejects one on an ASYMMETRIC_SIGN key, and the rejection is correct rather than an
# limitation — automatic rotation would mint a new version that every verifier holding the old
# public key would reject until it re-fetched. Rotation here is a deliberate sequence: add a
# version, publish the new public key, wait for verifiers, disable the old version.
resource "google_kms_crypto_key" "signing" {
  #checkov:skip=CKV_GCP_43:rotation_period is invalid on ASYMMETRIC_SIGN keys; rotation is a deliberate multi-step sequence documented in variables.tf.
  for_each = var.signing_keys

  name                          = each.key
  key_ring                      = google_kms_key_ring.this.id
  purpose                       = "ASYMMETRIC_SIGN"
  destroy_scheduled_duration    = "${each.value.destroy_scheduled_duration_seconds}s"
  skip_initial_version_creation = false
  deletion_policy               = "PREVENT"
  labels = merge(
    var.labels,
    each.value.labels,
    { managed-by = "terraform" },
  )

  version_template {
    algorithm        = each.value.algorithm
    protection_level = each.value.protection_level
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = length(merge(var.labels, each.value.labels, { managed-by = "terraform" })) <= 64
      error_message = "Merged global, key-specific, and managed-by labels must not exceed the Cloud KMS limit of 64."
    }
  }
}

locals {
  encrypter_decrypter_bindings = merge([
    for key_name, members in var.encrypter_decrypters : {
      for member in members : "${key_name}:${member}" => {
        key_name = key_name
        member   = member
      }
    }
  ]...)
}

resource "google_kms_crypto_key_iam_member" "encrypter_decrypter" {
  for_each = local.encrypter_decrypter_bindings

  crypto_key_id = google_kms_crypto_key.this[each.value.key_name].id
  role          = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  member        = each.value.member
}
