# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  baseline_labels = {
    data-classification = var.data_classification
    environment         = var.environment
    managed-by          = "terraform"
    owner               = var.owner
  }

  kms_key_location = var.kms_key_name == null ? null : split("/", var.kms_key_name)[3]
}

resource "google_kms_crypto_key_iam_member" "artifact_registry" {
  count = var.kms_key_name == null ? 0 : 1

  crypto_key_id = var.kms_key_name
  role          = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  member        = "serviceAccount:service-${var.project_number}@gcp-sa-artifactregistry.iam.gserviceaccount.com"
}

resource "google_artifact_registry_repository" "this" {
  project       = var.project_id
  location      = var.location
  repository_id = var.repository_id
  description   = var.description
  format        = "DOCKER"
  mode          = "STANDARD_REPOSITORY"
  kms_key_name  = var.kms_key_name
  labels        = merge(var.labels, local.baseline_labels)

  deletion_policy        = "PREVENT"
  cleanup_policy_dry_run = var.cleanup_policy_dry_run

  docker_config {
    immutable_tags = true
  }

  vulnerability_scanning_config {
    # INHERITED permits automatic scanning when the Container Scanning API is
    # enabled for the project. The computed state output must be verified.
    enablement_config = "INHERITED"
  }

  cleanup_policies {
    id     = "delete-stale-untagged"
    action = "DELETE"

    condition {
      tag_state  = "UNTAGGED"
      older_than = "${var.untagged_retention_days}d"
    }
  }

  cleanup_policies {
    id     = "keep-recent-versions"
    action = "KEEP"

    most_recent_versions {
      keep_count = var.minimum_versions_to_keep
    }
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = var.cleanup_policy_dry_run || var.cleanup_activation_approved
      error_message = "Disabling cleanup dry-run requires cleanup_activation_approved=true in the same reviewed change."
    }

    precondition {
      condition     = var.kms_key_name == null || local.kms_key_location == var.location
      error_message = "kms_key_name must use the same location as the Artifact Registry repository."
    }

    precondition {
      condition     = var.kms_key_name == null || var.project_number != null
      error_message = "A CMEK repository requires project_number so the exact Artifact Registry service agent can use the key."
    }
  }


  depends_on = [google_kms_crypto_key_iam_member.artifact_registry]
}
