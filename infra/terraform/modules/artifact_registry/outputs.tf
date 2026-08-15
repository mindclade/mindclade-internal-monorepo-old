# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "repository_name" {
  description = "Fully qualified Artifact Registry repository resource name"
  value       = google_artifact_registry_repository.this.name
}

output "repository_uri" {
  description = "Docker repository URI; publish and deploy immutable digest references beneath this URI"
  value       = "${var.location}-docker.pkg.dev/${var.project_id}/${var.repository_id}"
}

output "repository_id" {
  description = "Artifact Registry repository ID"
  value       = google_artifact_registry_repository.this.repository_id
}

output "cleanup_policy_dry_run" {
  description = "Whether cleanup policies are prevented from deleting artifacts"
  value       = google_artifact_registry_repository.this.cleanup_policy_dry_run
}

output "vulnerability_scanning_state" {
  description = "Provider-reported effective vulnerability-scanning state; verify this after deployment"
  value       = try(google_artifact_registry_repository.this.vulnerability_scanning_config[0].enablement_state, null)
}

output "vulnerability_scanning_state_reason" {
  description = "Provider-reported reason for the effective vulnerability-scanning state"
  value       = try(google_artifact_registry_repository.this.vulnerability_scanning_config[0].enablement_state_reason, null)
}

output "cleanup_contract" {
  description = "Reviewed cleanup-policy inputs for change-control and validation evidence"
  value = {
    dry_run                  = var.cleanup_policy_dry_run
    minimum_versions_to_keep = var.minimum_versions_to_keep
    tagged_version_retention = "retained-while-immutably-tagged"
    untagged_retention_days  = var.untagged_retention_days
  }
}
