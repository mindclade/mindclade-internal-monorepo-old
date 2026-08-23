# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

locals {
  baseline_labels = {
    "data-classification" = var.data_classification
    environment           = var.environment
    "managed-by"          = "terraform"
    owner                 = var.owner
    source                = "immutable-gcs-artifact"
  }
}

resource "google_compute_image" "this" {
  project           = var.project_id
  name              = var.name
  description       = "${var.description} Source SHA-256 ${var.source_sha256}; image contract SHA-256 ${var.image_contract_sha256}; GCS generation ${var.source_object_generation}."
  deletion_policy   = "PREVENT"
  labels            = merge(var.labels, local.baseline_labels)
  storage_locations = var.storage_locations

  raw_disk {
    container_type = "TAR"
    source         = var.source_uri
  }

  image_encryption_key {
    kms_key_self_link       = var.kms_key_name
    kms_key_service_account = var.compute_service_account_email
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = var.qualification_state == "qualified-v1"
      error_message = "qualification_state must be qualified-v1 before Terraform may create an image."
    }

    precondition {
      condition     = strcontains(var.source_uri, var.source_sha256)
      error_message = "source_uri must contain source_sha256 so the object name is content-addressed."
    }

    precondition {
      condition     = startswith(var.source_uri, "https://storage.googleapis.com/${var.source_bucket_name}/")
      error_message = "source_uri must belong to the exact Terraform-owned source_bucket_name."
    }

    precondition {
      condition     = !strcontains(var.source_uri, "latest") && !strcontains(var.source_uri, "current")
      error_message = "source_uri must not contain a mutable latest/current alias."
    }
  }
}
