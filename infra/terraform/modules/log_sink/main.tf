# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# Organization-level log sinks, and the destinations they write into.
#
# The ordering here is the part that goes wrong silently. A sink is created with a writer
# identity that Google mints at creation time; that identity has no permission on the
# destination until something grants it. A sink whose writer cannot write does not error —
# it reports as healthy and drops every entry, and the gap is only visible as an absence
# nobody is looking for.
#
# So: destination, then sink, then the IAM binding that connects them, with the binding
# depending on both. `unique_writer_identity = true` throughout, because the shared default
# writer would give every sink the same principal and make a per-sink grant meaningless.

locals {
  logging_sinks = { for k, v in var.sinks : k => v if v.destination == "logging" }
  storage_sinks = { for k, v in var.sinks : k => v if v.destination == "storage" }

  # Bucket location for a Cloud Logging bucket is fixed at creation and cannot be changed.
  # It is taken from the storage bucket's location where one is given, so that a caller
  # setting a region for its cold tier does not silently get a global hot tier.
  logging_location = "global"
}

# ---------------------------------------------------------------------------------------
# Destinations
# ---------------------------------------------------------------------------------------

resource "google_logging_project_bucket_config" "this" {
  for_each = local.logging_sinks

  project        = var.project_id
  location       = local.logging_location
  bucket_id      = each.key
  description    = each.value.description
  retention_days = each.value.retention_days

  # Irreversible after creation: turning analytics on later requires deleting the bucket,
  # which takes every entry in it.
  enable_analytics = each.value.enable_analytics
}

resource "google_storage_bucket" "this" {
  for_each = local.storage_sinks

  project  = var.project_id
  name     = each.value.bucket.name
  location = each.value.bucket.location
  labels   = var.labels

  # Log archives are append-only by nature and there is no reason for a bucket-level ACL.
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  # A log archive that can be silently overwritten is not an archive. Versioning costs
  # little here because log objects are written once.
  versioning {
    enabled = true
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
      # Deliberately not locked here. A locked retention policy cannot be shortened or
      # removed by anyone, ever, including to correct a mistake — locking is a separate,
      # explicit operational act rather than something a module does on a caller's behalf.
      is_locked = false
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
}

# ---------------------------------------------------------------------------------------
# Sinks
# ---------------------------------------------------------------------------------------

resource "google_logging_organization_sink" "logging" {
  for_each = local.logging_sinks

  name             = each.key
  description      = each.value.description
  org_id           = trimprefix(var.parent, "organizations/")
  include_children = var.include_children
  filter           = each.value.filter

  destination = "logging.googleapis.com/${google_logging_project_bucket_config.this[each.key].id}"

  dynamic "exclusions" {
    for_each = each.value.exclusions

    content {
      name        = exclusions.value.name
      description = exclusions.value.description
      filter      = exclusions.value.filter
    }
  }
}

resource "google_logging_organization_sink" "storage" {
  for_each = local.storage_sinks

  name             = each.key
  description      = each.value.description
  org_id           = trimprefix(var.parent, "organizations/")
  include_children = var.include_children
  filter           = each.value.filter

  destination = "storage.googleapis.com/${google_storage_bucket.this[each.key].name}"

  dynamic "exclusions" {
    for_each = each.value.exclusions

    content {
      name        = exclusions.value.name
      description = exclusions.value.description
      filter      = exclusions.value.filter
    }
  }
}

# ---------------------------------------------------------------------------------------
# The grants that make the sinks actually deliver
# ---------------------------------------------------------------------------------------
# Without these the sinks exist, report healthy, and write nothing.

resource "google_project_iam_member" "logging_writer" {
  for_each = local.logging_sinks

  project = var.project_id
  role    = "roles/logging.bucketWriter"
  member  = google_logging_organization_sink.logging[each.key].writer_identity

  # A condition scoping the grant to this one bucket. Without it, every sink's writer could
  # write into every other sink's bucket, which defeats the point of a unique writer.
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
  member = google_logging_organization_sink.storage[each.key].writer_identity
}

# ---------------------------------------------------------------------------------------
# Each project's own _Default bucket
# ---------------------------------------------------------------------------------------
# Every project writes a second copy of everything above into its own _Default bucket.
# Shortening its retention is what stops each project paying to store logs this module is
# already keeping centrally, while leaving enough for `gcloud logging read` during an
# incident.

resource "google_logging_project_bucket_config" "default" {
  count = var.default_sink_retention_days > 0 ? 1 : 0

  project        = var.project_id
  location       = "global"
  bucket_id      = "_Default"
  retention_days = var.default_sink_retention_days
}
