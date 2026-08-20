# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# Aggregated organization- or folder-level sinks and their destinations. Destination
# permissions are additive, and every sink receives its own writer identity.

locals {
  is_organization = startswith(var.parent, "organizations/")

  logging_sinks = { for name, sink in var.sinks : name => sink if sink.destination == "logging" }
  storage_sinks = { for name, sink in var.sinks : name => sink if sink.destination == "storage" }

  organization_logging_sinks = local.is_organization ? local.logging_sinks : {}
  organization_storage_sinks = local.is_organization ? local.storage_sinks : {}
  folder_logging_sinks       = local.is_organization ? {} : local.logging_sinks
  folder_storage_sinks       = local.is_organization ? {} : local.storage_sinks
}

# ---------------------------------------------------------------------------------------
# Destinations
# ---------------------------------------------------------------------------------------

resource "google_logging_project_bucket_config" "this" {
  for_each = local.logging_sinks

  project          = var.project_id
  location         = var.logging_bucket_location
  bucket_id        = each.key
  description      = each.value.description
  retention_days   = each.value.retention_days
  enable_analytics = each.value.enable_analytics
  deletion_policy  = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_storage_bucket" "this" {
  for_each = local.storage_sinks

  project                     = var.project_id
  name                        = each.value.bucket.name
  location                    = each.value.bucket.location
  labels                      = var.labels
  force_destroy               = false
  deletion_policy             = "PREVENT"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  versioning {
    enabled = true
  }

  soft_delete_policy {
    retention_duration_seconds = each.value.bucket.soft_delete_retention_days * 86400
  }

  dynamic "encryption" {
    for_each = each.value.bucket.encryption_key == null ? [] : [each.value.bucket.encryption_key]

    content {
      default_kms_key_name = encryption.value
    }
  }

  dynamic "retention_policy" {
    for_each = each.value.bucket.retention_days == null ? [] : [each.value.bucket.retention_days]

    content {
      retention_period = retention_policy.value * 86400
      is_locked        = each.value.bucket.lock_retention_policy
    }
  }

  dynamic "lifecycle_rule" {
    for_each = each.value.bucket.lifecycle_rules

    content {
      condition {
        age = lifecycle_rule.value.age
      }

      action {
        type          = lifecycle_rule.value.action
        storage_class = lifecycle_rule.value.storage_class
      }
    }
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition = !each.value.bucket.lock_retention_policy || (
        each.value.bucket.retention_days != null &&
        var.retention_lock_confirmation == "LOCKING A CLOUD STORAGE RETENTION POLICY IS IRREVERSIBLE"
      )
      error_message = "A locked archive requires retention_days and the exact retention_lock_confirmation acknowledgement. Bucket retention locking is irreversible."
    }
  }
}

# ---------------------------------------------------------------------------------------
# Aggregated sinks. Organization and folder resources are deliberately separate because
# their parent arguments and resource identities are not interchangeable.
# ---------------------------------------------------------------------------------------

resource "google_logging_organization_sink" "logging" {
  for_each = local.organization_logging_sinks

  name             = each.key
  description      = each.value.description
  org_id           = trimprefix(var.parent, "organizations/")
  include_children = var.include_children
  filter           = each.value.filter
  destination      = "logging.googleapis.com/${google_logging_project_bucket_config.this[each.key].id}"

  dynamic "exclusions" {
    for_each = each.value.exclusions

    content {
      name        = exclusions.value.name
      description = exclusions.value.description
      filter      = exclusions.value.filter
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_logging_organization_sink" "storage" {
  for_each = local.organization_storage_sinks

  name             = each.key
  description      = each.value.description
  org_id           = trimprefix(var.parent, "organizations/")
  include_children = var.include_children
  filter           = each.value.filter
  destination      = "storage.googleapis.com/${google_storage_bucket.this[each.key].name}"

  dynamic "exclusions" {
    for_each = each.value.exclusions

    content {
      name        = exclusions.value.name
      description = exclusions.value.description
      filter      = exclusions.value.filter
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_logging_folder_sink" "logging" {
  for_each = local.folder_logging_sinks

  name             = each.key
  description      = each.value.description
  folder           = var.parent
  include_children = var.include_children
  filter           = each.value.filter
  destination      = "logging.googleapis.com/${google_logging_project_bucket_config.this[each.key].id}"

  dynamic "exclusions" {
    for_each = each.value.exclusions

    content {
      name        = exclusions.value.name
      description = exclusions.value.description
      filter      = exclusions.value.filter
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_logging_folder_sink" "storage" {
  for_each = local.folder_storage_sinks

  name             = each.key
  description      = each.value.description
  folder           = var.parent
  include_children = var.include_children
  filter           = each.value.filter
  destination      = "storage.googleapis.com/${google_storage_bucket.this[each.key].name}"

  dynamic "exclusions" {
    for_each = each.value.exclusions

    content {
      name        = exclusions.value.name
      description = exclusions.value.description
      filter      = exclusions.value.filter
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}

locals {
  logging_writer_identities = merge(
    { for name, sink in google_logging_organization_sink.logging : name => sink.writer_identity },
    { for name, sink in google_logging_folder_sink.logging : name => sink.writer_identity },
  )
  storage_writer_identities = merge(
    { for name, sink in google_logging_organization_sink.storage : name => sink.writer_identity },
    { for name, sink in google_logging_folder_sink.storage : name => sink.writer_identity },
  )
}

# ---------------------------------------------------------------------------------------
# Additive destination grants. Without these, an otherwise healthy sink drops delivery.
# ---------------------------------------------------------------------------------------

resource "google_project_iam_member" "logging_writer" {
  for_each = local.logging_sinks

  project = var.project_id
  role    = "roles/logging.bucketWriter"
  member  = local.logging_writer_identities[each.key]

  condition {
    title       = "only-${each.key}"
    description = "Restricts this writer to the ${each.key} log bucket."
    expression  = "resource.name.endsWith('/buckets/${each.key}')"
  }
}

resource "google_storage_bucket_iam_member" "storage_writer" {
  for_each = local.storage_sinks

  bucket = google_storage_bucket.this[each.key].name
  role   = "roles/storage.objectCreator"
  member = local.storage_writer_identities[each.key]
}

# This resource manages only the destination project's _Default bucket. Aggregated sinks
# cannot reach into every descendant project's local bucket configuration.
resource "google_logging_project_bucket_config" "default" {
  count = var.default_sink_retention_days > 0 ? 1 : 0

  project         = var.project_id
  location        = "global"
  bucket_id       = "_Default"
  retention_days  = var.default_sink_retention_days
  deletion_policy = "ABANDON"
}
