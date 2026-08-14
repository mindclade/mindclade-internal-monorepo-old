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
