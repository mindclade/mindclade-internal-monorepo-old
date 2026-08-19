# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# Access Transparency: the record of Google personnel reading customer content, and the alert
# that makes somebody look at it.
#
# The export and the alert are both here because either alone is close to useless. A bucket
# nobody reads is evidence for an investigation that may never start; an alert with no
# durable record is a notification somebody dismissed. Together they answer "did this happen,
# and was it justified" months later.
#
# Enabling Access Transparency itself is an organization-level entitlement tied to a support
# plan. It is not a Terraform resource and is not attempted here — this module handles what
# happens to the logs once the entitlement exists.

resource "google_storage_bucket" "access_transparency" {
  project  = var.project_id
  name     = var.sink.bucket.name
  location = var.sink.bucket.location
  labels   = var.labels

  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  versioning {
    enabled = true
  }

  dynamic "encryption" {
    for_each = var.sink.bucket.encryption_key == null ? [] : [var.sink.bucket.encryption_key]

    content {
      default_kms_key_name = encryption.value
    }
  }

  # Retention, not lifecycle deletion. A retention policy stops an object being deleted
  # before its window expires — including by whoever is being investigated.
  retention_policy {
    retention_period = coalesce(var.sink.bucket.retention_days, 2555) * 86400
    is_locked        = false
  }
}

resource "google_logging_organization_sink" "access_transparency" {
  name             = var.sink.name
  description      = "Access Transparency entries: Google personnel access to customer content."
  org_id           = var.org_id
  include_children = true
  filter           = var.sink.filter

  destination = "storage.googleapis.com/${google_storage_bucket.access_transparency.name}"
}

# Without this the sink exists, reports healthy, and writes nothing.
resource "google_storage_bucket_iam_member" "writer" {
  bucket = google_storage_bucket.access_transparency.name
  role   = "roles/storage.objectCreator"
  member = google_logging_organization_sink.access_transparency.writer_identity
}

# ---------------------------------------------------------------------------------------
# Alerting
# ---------------------------------------------------------------------------------------

resource "google_monitoring_notification_channel" "this" {
  for_each = var.alert == null ? toset([]) : toset(var.alert.notification_channels)

  project      = var.project_id
  display_name = each.value
  type         = "email"

  labels = {
    email_address = each.value
  }
}

resource "google_monitoring_alert_policy" "access" {
  count = var.alert == null ? 0 : 1

  project      = var.project_id
  display_name = var.alert.display_name
  combiner     = "OR"
  severity     = var.alert.severity

  notification_channels = [
    for c in var.alert.notification_channels :
    google_monitoring_notification_channel.this[c].id
  ]

  # A log-match condition rather than a metric threshold. A metric needs an aggregation
  # window, and any window at all turns "somebody read customer data" into a count — which
  # is the one framing that makes the individual justification unreadable.
  conditions {
    display_name = "Access Transparency entry written"

    condition_matched_log {
      filter = var.alert.filter
    }
  }

  alert_strategy {
    # Required for a log-match condition. Set to the shortest permitted period rather than
    # something larger: the volume is a handful of entries a year, so rate limiting exists
    # here to satisfy the API, not to suppress anything.
    notification_rate_limit {
      period = "300s"
    }

    auto_close = "604800s"
  }

  documentation {
    content   = var.alert.documentation
    mime_type = "text/markdown"
  }
}
