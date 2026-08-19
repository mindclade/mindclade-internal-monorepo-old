# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

variables {
  parent     = "organizations/123456789012"
  project_id = "mc-b-audit-001"
}

run "a_logging_sink_writes_into_its_own_bucket" {
  command = plan

  variables {
    sinks = {
      application-hot = {
        description      = "Application logs."
        destination      = "logging"
        enable_analytics = true
        retention_days   = 30
        filter           = "resource.type=\"k8s_container\""
      }
    }
  }

  assert {
    condition     = google_logging_project_bucket_config.this["application-hot"].retention_days == 30
    error_message = "Retention must be carried through to the log bucket."
  }

  assert {
    condition     = google_logging_project_bucket_config.this["application-hot"].enable_analytics == true
    error_message = "Log Analytics can only be set at creation, so it must be honoured on the first apply."
  }
}

run "every_sink_gets_a_unique_writer_scoped_to_one_bucket" {
  command = plan

  variables {
    sinks = {
      application-hot = {
        description    = "Application logs."
        destination    = "logging"
        retention_days = 30
        filter         = "resource.type=\"k8s_container\""
      }
    }
  }

  # A sink whose writer cannot write reports healthy and drops every entry. The binding is
  # the thing that makes the sink real, and the condition is what stops one sink's writer
  # reaching another sink's bucket.
  assert {
    condition     = google_project_iam_member.logging_writer["application-hot"].role == "roles/logging.bucketWriter"
    error_message = "Without roles/logging.bucketWriter the sink silently delivers nothing."
  }

  assert {
    condition     = length(google_project_iam_member.logging_writer["application-hot"].condition) == 1
    error_message = "The writer grant must be scoped by condition to this sink's bucket alone."
  }
}

run "a_storage_sink_builds_an_archive_bucket" {
  command = plan

  variables {
    sinks = {
      application-archive = {
        description = "Seven-year cold tier."
        destination = "storage"
        filter      = "resource.type=\"k8s_container\""
        bucket = {
          name           = "mc-app-logs-archive"
          location       = "EUR4"
          encryption_key = "projects/p/locations/l/keyRings/r/cryptoKeys/logs"
          retention_days = 2555
          lifecycle_rules = [
            { age = 30, action = "SetStorageClass", storage_class = "NEARLINE" },
          ]
        }
      }
    }
  }

  assert {
    condition     = google_storage_bucket.this["application-archive"].uniform_bucket_level_access == true
    error_message = "A log archive must not carry object ACLs."
  }

  assert {
    condition     = google_storage_bucket.this["application-archive"].retention_policy[0].retention_period == tostring(2555 * 86400)
    error_message = "Retention days must be converted to seconds for the bucket policy."
  }

  # Locking is irreversible by anyone including to correct a mistake, so it is an explicit
  # operational act rather than something this module does on a caller's behalf.
  assert {
    condition     = google_storage_bucket.this["application-archive"].retention_policy[0].is_locked == false
    error_message = "The module must not lock a retention policy implicitly."
  }

  assert {
    condition     = google_storage_bucket_iam_member.storage_writer["application-archive"].role == "roles/storage.objectCreator"
    error_message = "The archive writer needs objectCreator, not objectAdmin — a log archive is append-only."
  }
}

run "a_storage_sink_without_a_bucket_is_rejected" {
  command = plan

  variables {
    sinks = {
      broken = {
        description = "No destination."
        destination = "storage"
        filter      = "resource.type=\"k8s_container\""
      }
    }
  }

  expect_failures = [var.sinks]
}

run "an_empty_filter_is_rejected" {
  command = plan

  variables {
    sinks = {
      catch-all = {
        description = "Everything."
        destination = "logging"
        filter      = "   "
      }
    }
  }

  expect_failures = [var.sinks]
}

run "default_bucket_retention_can_be_left_alone" {
  command = plan

  variables {
    sinks                       = {}
    default_sink_retention_days = 0
  }

  assert {
    condition     = length(google_logging_project_bucket_config.default) == 0
    error_message = "Zero must mean leave _Default untouched, not set it to zero."
  }
}
