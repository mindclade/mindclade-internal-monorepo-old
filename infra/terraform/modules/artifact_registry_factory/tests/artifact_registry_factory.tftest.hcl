# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

mock_provider "google" {}

run "digest_first_repository_collection" {
  command = plan
  variables {
    project_id     = "mindclade-staging-platform"
    location       = "us-central1"
    encryption_key = "projects/mindclade-seed/locations/us-central1/keyRings/staging/cryptoKeys/storage"
    repositories = {
      containers = {
        format        = "DOCKER"
        description   = "Immutable workload images"
        docker_config = { immutable_tags = true }
        cleanup_policies = {
          delete-untagged = { action = "DELETE", condition_state = "UNTAGGED", older_than = "2592000s" }
          keep-recent     = { action = "KEEP", most_recent_versions = 20 }
        }
      }
      python = { format = "PYTHON", description = "Private Python packages" }
    }
  }
  assert {
    condition     = google_artifact_registry_repository.this["containers"].cleanup_policy_dry_run
    error_message = "Cleanup policies must begin in dry-run mode."
  }
}
