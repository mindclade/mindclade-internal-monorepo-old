# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

mock_provider "google" {}

variables {
  parent     = "organizations/123456789012"
  project_id = "mc-b-audit-001"
}

run "organization_logging_sink_uses_organization_resource" {
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
    condition = (
      length(google_logging_organization_sink.logging) == 1 &&
      length(google_logging_folder_sink.logging) == 0
    )
    error_message = "An organization parent must select only an organization sink."
  }

  assert {
    condition = (
      google_logging_project_bucket_config.this["application-hot"].retention_days == 30 &&
      google_logging_project_bucket_config.this["application-hot"].enable_analytics == true &&
      google_logging_project_bucket_config.this["application-hot"].deletion_policy == "PREVENT"
    )
    error_message = "The queryable destination must retain analytics and deletion safeguards."
  }

  assert {
    condition = (
      google_project_iam_member.logging_writer["application-hot"].role == "roles/logging.bucketWriter" &&
      length(google_project_iam_member.logging_writer["application-hot"].condition) == 1
    )
    error_message = "The unique writer must receive only a bucket-scoped Logging grant."
  }
}

run "folder_storage_sink_uses_folder_resource" {
  command = plan

  variables {
    parent = "folders/123456789012"
    sinks = {
      application-archive = {
        description = "Seven-year cold tier."
        destination = "storage"
        filter      = "resource.type=\"k8s_container\""
        bucket = {
          name                   = "mc-app-logs-archive"
          location               = "US"
          access_log_bucket_name = "mc-central-storage-access"
          encryption_key         = "projects/security/locations/us/keyRings/logs/cryptoKeys/archive"
          retention_days         = 2555
        }
      }
    }
  }

  assert {
    condition = (
      length(google_logging_organization_sink.storage) == 0 &&
      length(google_logging_folder_sink.storage) == 1 &&
      google_logging_folder_sink.storage["application-archive"].folder == "folders/123456789012"
    )
    error_message = "A folder parent must select only the folder sink resource."
  }

  assert {
    condition = (
      google_storage_bucket.this["application-archive"].uniform_bucket_level_access == true &&
      google_storage_bucket.this["application-archive"].public_access_prevention == "enforced" &&
      google_storage_bucket.this["application-archive"].force_destroy == false &&
      google_storage_bucket.this["application-archive"].deletion_policy == "PREVENT" &&
      google_storage_bucket.this["application-archive"].logging[0].log_bucket == "mc-central-storage-access" &&
      google_storage_bucket.this["application-archive"].retention_policy[0].is_locked == false
    )
    error_message = "A generic archive must be private and deletion guarded without an implicit irreversible lock."
  }

  assert {
    condition     = google_storage_bucket_iam_member.storage_writer["application-archive"].role == "roles/storage.objectCreator"
    error_message = "Archive writers require append-only objectCreator, not objectAdmin."
  }


  assert {
    condition     = output.required_access_log_writer_grants["application-archive"].member == "group:cloud-storage-analytics@google.com"
    error_message = "The access-log bucket owner must receive the exact required writer principal."
  }
}

run "explicit_retention_lock_is_honored" {
  command = plan

  variables {
    retention_lock_confirmation = "LOCKING A CLOUD STORAGE RETENTION POLICY IS IRREVERSIBLE"
    sinks = {
      audit-archive = {
        description = "Immutable audit records."
        destination = "storage"
        filter      = "log_id(\"cloudaudit.googleapis.com/activity\")"
        bucket = {
          name                   = "mc-audit-archive"
          location               = "US"
          access_log_bucket_name = "mc-central-storage-access"
          retention_days         = 2555
          lock_retention_policy  = true
        }
      }
    }
  }

  assert {
    condition     = google_storage_bucket.this["audit-archive"].retention_policy[0].is_locked == true
    error_message = "An explicitly approved retention lock must reach the archive bucket."
  }
}

run "reject_retention_lock_without_confirmation" {
  command = plan

  variables {
    sinks = {
      audit-archive = {
        description = "Immutable audit records."
        destination = "storage"
        filter      = "log_id(\"cloudaudit.googleapis.com/activity\")"
        bucket = {
          name                   = "mc-audit-archive"
          location               = "US"
          access_log_bucket_name = "mc-central-storage-access"
          retention_days         = 2555
          lock_retention_policy  = true
        }
      }
    }
  }

  expect_failures = [google_storage_bucket.this["audit-archive"]]
}

run "reject_storage_sink_without_bucket" {
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

run "reject_empty_filter" {
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

run "default_bucket_management_is_destination_project_only" {
  command = plan

  variables {
    sinks                       = {}
    default_sink_retention_days = 14
  }

  assert {
    condition = (
      length(google_logging_project_bucket_config.default) == 1 &&
      google_logging_project_bucket_config.default[0].project == "mc-b-audit-001" &&
      output.default_bucket_scope.project == "mc-b-audit-001"
    )
    error_message = "Only the destination project's own _Default bucket may be managed."
  }
}

run "default_bucket_can_be_left_unmanaged" {
  command = plan

  variables {
    sinks                       = {}
    default_sink_retention_days = 0
  }

  assert {
    condition = (
      length(google_logging_project_bucket_config.default) == 0 &&
      output.default_bucket_scope == null
    )
    error_message = "Zero must leave the destination project's _Default bucket unmanaged."
  }
}
