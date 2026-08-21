# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

output "repositories" {
  description = "Repository names and immutable publication roots keyed by caller identity."
  value = {
    for key, repository in google_artifact_registry_repository.this : key => {
      id   = repository.repository_id
      name = repository.name
      uri  = "${var.location}-${repository.format == "DOCKER" ? "docker" : lower(repository.format)}.pkg.dev/${var.project_id}/${repository.repository_id}"
    }
  }
}

output "required_kms_grant" {
  description = "Exact cross-state grant the KMS owner must apply after resolving the project number."
  value = {
    crypto_key             = var.encryption_key
    service_agent_template = "service-PROJECT_NUMBER@gcp-sa-artifactregistry.iam.gserviceaccount.com"
    role                   = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  }
}
