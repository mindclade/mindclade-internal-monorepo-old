# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

run "secure_repository_contract" {
  command = plan

  variables {
    project_id          = "mindclade-production"
    location            = "us-central1"
    repository_id       = "application-images"
    environment         = "production"
    owner               = "cloud-platform"
    data_classification = "confidential"
    labels = {
      system     = "mindclade"
      managed-by = "somebody-else"
    }
  }

  assert {
    condition = (
      google_artifact_registry_repository.this.format == "DOCKER" &&
      google_artifact_registry_repository.this.mode == "STANDARD_REPOSITORY" &&
      google_artifact_registry_repository.this.deletion_policy == "PREVENT"
    )
    error_message = "The repository must remain a deletion-protected standard Docker repository."
  }

  assert {
    condition = (
      google_artifact_registry_repository.this.docker_config[0].immutable_tags == true &&
      google_artifact_registry_repository.this.vulnerability_scanning_config[0].enablement_config == "INHERITED"
    )
    error_message = "Docker tags must be immutable and repository scanning must inherit the enabled project setting."
  }

  assert {
    condition     = google_artifact_registry_repository.this.cleanup_policy_dry_run == true
    error_message = "Cleanup must start in dry-run mode."
  }

  assert {
    condition = one([
      for policy in google_artifact_registry_repository.this.cleanup_policies : policy
      if policy.id == "delete-stale-untagged"
    ]).condition[0].older_than == "30d"
    error_message = "The default delete policy must target only stale untagged versions after 30 days."
  }

  assert {
    condition = one([
      for policy in google_artifact_registry_repository.this.cleanup_policies : policy
      if policy.id == "keep-recent-versions"
    ]).most_recent_versions[0].keep_count == 20
    error_message = "The cleanup contract must retain at least the default 20 recent versions per package."
  }

  assert {
    condition = (
      google_artifact_registry_repository.this.labels["managed-by"] == "terraform" &&
      google_artifact_registry_repository.this.labels["owner"] == "cloud-platform" &&
      google_artifact_registry_repository.this.labels["data-classification"] == "confidential" &&
      google_artifact_registry_repository.this.labels["system"] == "mindclade"
    )
    error_message = "Baseline governance labels must take precedence while retaining caller labels."
  }
}

run "cleanup_activation_requires_approval" {
  command = plan

  variables {
    project_id             = "mindclade-production"
    location               = "us-central1"
    repository_id          = "application-images"
    environment            = "production"
    owner                  = "cloud-platform"
    cleanup_policy_dry_run = false
  }

  expect_failures = [
    google_artifact_registry_repository.this,
  ]
}

run "cleanup_activation_can_be_reviewed_explicitly" {
  command = plan

  variables {
    project_id                  = "mindclade-production"
    location                    = "us-central1"
    repository_id               = "application-images"
    environment                 = "production"
    owner                       = "cloud-platform"
    cleanup_policy_dry_run      = false
    cleanup_activation_approved = true
    untagged_retention_days     = 45
    minimum_versions_to_keep    = 25
  }

  assert {
    condition = (
      google_artifact_registry_repository.this.cleanup_policy_dry_run == false &&
      output.cleanup_contract.untagged_retention_days == 45 &&
      output.cleanup_contract.minimum_versions_to_keep == 25 &&
      output.cleanup_contract.tagged_version_retention == "retained-while-immutably-tagged"
    )
    error_message = "An approved cleanup plan must retain its reviewed inputs and surface immutable tagged-version retention."
  }
}
