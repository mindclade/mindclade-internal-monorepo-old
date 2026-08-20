# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

variables {
  org_id     = "123456789012"
  project_id = "mc-b-audit-001"

  sink = {
    name   = "mc-access-transparency"
    filter = "logName:\"logs/cloudaudit.googleapis.com%2Faccess_transparency\""
    bucket = {
      name           = "mc-access-transparency"
      location       = "EUR4"
      encryption_key = "projects/p/locations/l/keyRings/r/cryptoKeys/logs"
      retention_days = 2555
    }
  }
}

run "records_cannot_be_deleted_before_their_window_expires" {
  command = plan

  # Retention, not a lifecycle rule. The distinction matters: a retention policy stops an
  # object being deleted by anyone, including whoever is being investigated.
  assert {
    condition     = google_storage_bucket.access_transparency.retention_policy[0].retention_period == tostring(2555 * 86400)
    error_message = "Retention must be set on the bucket, in seconds."
  }

  assert {
    condition = (
      google_storage_bucket.access_transparency.public_access_prevention == "enforced" &&
      google_storage_bucket.access_transparency.deletion_policy == "PREVENT" &&
      google_storage_bucket.access_transparency.force_destroy == false &&
      google_storage_bucket.access_transparency.soft_delete_policy[0].retention_duration_seconds == 7776000
    )
    error_message = "An access-transparency archive must remain private and protected against early or accidental deletion."
  }
}

run "the_sink_writer_is_granted_on_the_bucket" {
  command = plan

  assert {
    condition     = google_storage_bucket_iam_member.writer.role == "roles/storage.objectCreator"
    error_message = "Without the grant the sink reports healthy and writes nothing."
  }
}

run "a_filter_that_does_not_select_access_transparency_is_rejected" {
  command = plan

  variables {
    sink = {
      name   = "mc-access-transparency"
      filter = "resource.type=\"gce_instance\""
      bucket = {
        name     = "mc-access-transparency"
        location = "EUR4"
      }
    }
  }

  # An empty bucket is indistinguishable from an estate nobody accessed, which is exactly the
  # wrong conclusion to draw silently.
  expect_failures = [var.sink]
}

run "a_short_retention_window_is_rejected" {
  command = plan

  variables {
    sink = {
      name   = "mc-access-transparency"
      filter = "logName:\"logs/cloudaudit.googleapis.com%2Faccess_transparency\""
      bucket = {
        name           = "mc-access-transparency"
        location       = "EUR4"
        retention_days = 30
      }
    }
  }

  expect_failures = [var.sink]
}

run "an_alert_with_no_channels_is_rejected" {
  command = plan

  variables {
    alert = {
      display_name          = "Google personnel accessed customer content"
      filter                = "logName:\"logs/cloudaudit.googleapis.com%2Faccess_transparency\""
      notification_channels = []
    }
  }

  # Google accepts it, the policy shows as enabled, and the first anyone knows is that a page
  # never arrived.
  expect_failures = [var.alert]
}

run "every_access_alerts_with_no_aggregation_window" {
  command = plan

  variables {
    alert = {
      display_name          = "Google personnel accessed customer content"
      severity              = "WARNING"
      filter                = "logName:\"logs/cloudaudit.googleapis.com%2Faccess_transparency\""
      notification_channels = ["security@mindclade.com"]
      documentation         = "Check whether there is an open support case."
    }
  }

  # A metric threshold would need an aggregation window, and any window turns "somebody read
  # customer data" into a count.
  assert {
    condition     = length(google_monitoring_alert_policy.access[0].conditions[0].condition_matched_log) == 1
    error_message = "The alert must use a log-match condition rather than a metric threshold."
  }

  assert {
    condition     = length(google_monitoring_notification_channel.this) == 1
    error_message = "A notification channel must be created for each address."
  }
}
