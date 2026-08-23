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
  mode          = each.value.mode
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

  # The repository format selects the upstream block, so an upstream can never be attached to
  # a format that would ignore it. disable_upstream_validation is deliberately never set: the
  # provider default (false) makes Google prove the upstream resolves at create time, which is
  # the only cheap check that a typo in upstream_path did not produce a repository that exists,
  # plans clean, and returns 404 to every client on the first fetch.
  dynamic "remote_repository_config" {
    for_each = each.value.remote_repository_config == null ? [] : [each.value.remote_repository_config]
    content {
      description = remote_repository_config.value.description

      dynamic "apt_repository" {
        for_each = each.value.format == "APT" ? [remote_repository_config.value] : []
        content {
          public_repository {
            repository_base = apt_repository.value.public_upstream
            repository_path = apt_repository.value.upstream_path
          }
        }
      }

      dynamic "yum_repository" {
        for_each = each.value.format == "YUM" ? [remote_repository_config.value] : []
        content {
          public_repository {
            repository_base = yum_repository.value.public_upstream
            repository_path = yum_repository.value.upstream_path
          }
        }
      }

      dynamic "maven_repository" {
        for_each = each.value.format == "MAVEN" ? [remote_repository_config.value] : []
        content {
          public_repository = maven_repository.value.public_upstream
        }
      }

      dynamic "npm_repository" {
        for_each = each.value.format == "NPM" ? [remote_repository_config.value] : []
        content {
          public_repository = npm_repository.value.public_upstream
        }
      }

      dynamic "python_repository" {
        for_each = each.value.format == "PYTHON" ? [remote_repository_config.value] : []
        content {
          public_repository = python_repository.value.public_upstream
        }
      }
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
    # Declaring a proxy is a supply-chain decision, not a repository-shape detail: Google
    # begins fetching from a public upstream on its own network and republishes the result
    # under a pkg.dev name that clients and reviewers read as first-party. The acknowledgement
    # is collection-wide and defaults to false so a remote repository can never arrive as an
    # incidental line in a repositories map that nobody read as an egress change.
    precondition {
      condition     = each.value.mode != "REMOTE_REPOSITORY" || var.remote_upstream_egress_approved
      error_message = "Proxying a public upstream requires remote_upstream_egress_approved=true in the same reviewed change."
    }
  }
}
