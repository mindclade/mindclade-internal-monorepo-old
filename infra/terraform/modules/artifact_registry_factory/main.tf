# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

resource "google_artifact_registry_repository" "this" {
  for_each = var.repositories

  project       = var.project_id
  location      = var.location
  repository_id = each.key
  description   = each.value.description
  format        = each.value.format
  mode          = "STANDARD_REPOSITORY"
  kms_key_name  = var.encryption_key
  labels        = merge(var.labels, { managed-by = "terraform" })

  deletion_policy        = "PREVENT"
  cleanup_policy_dry_run = true

  dynamic "docker_config" {
    for_each = each.value.format == "DOCKER" ? [coalesce(each.value.docker_config, { immutable_tags = true })] : []
    content {
      immutable_tags = docker_config.value.immutable_tags
    }
  }

  dynamic "vulnerability_scanning_config" {
    for_each = each.value.format == "DOCKER" && var.enable_vulnerability_scanning ? [1] : []
    content {
      enablement_config = "INHERITED"
    }
  }

  dynamic "cleanup_policies" {
    for_each = each.value.cleanup_policies
    content {
      id     = cleanup_policies.key
      action = cleanup_policies.value.action

      dynamic "condition" {
        for_each = cleanup_policies.value.most_recent_versions == null ? [cleanup_policies.value] : []
        content {
          tag_state  = condition.value.condition_state
          older_than = condition.value.older_than
        }
      }

      dynamic "most_recent_versions" {
        for_each = cleanup_policies.value.most_recent_versions == null ? [] : [cleanup_policies.value.most_recent_versions]
        content {
          keep_count = most_recent_versions.value
        }
      }
    }
  }

  lifecycle {
    prevent_destroy = true
    precondition {
      condition     = split("/", var.encryption_key)[3] == var.location
      error_message = "encryption_key location must match the Artifact Registry location."
    }
  }
}
