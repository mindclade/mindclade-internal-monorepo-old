# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

variables {
  project_id        = "mindclade-production"
  bucket_name       = "mindclade-production-bazel-cache"
  location          = "us-central1"
  kms_key_name      = "projects/mindclade-security/locations/us-central1/keyRings/build-cache/cryptoKeys/bazel"
  access_log_bucket = "mindclade-central-storage-logs"
  environment       = "production"
  owner             = "developer-platform"
  reader_members = [
    "principalSet://iam.googleapis.com/projects/123456789/locations/global/workloadIdentityPools/mindclade-production.svc.id.goog/namespace/builds",
  ]
  writer_members = [
    "serviceAccount:bazel-cache@mindclade-production.iam.gserviceaccount.com",
  ]
}

run "rebuildable_cache_contract" {
  command = plan

  assert {
    condition = (
      output.gs_uri == "gs://mindclade-production-bazel-cache" &&
      output.cache_policy.data_lifecycle == "rebuildable" &&
      output.cache_policy.data_classification == "internal" &&
      output.cache_policy.versioning_enabled == true &&
      output.cache_policy.cache_ttl_days == 14 &&
      output.cache_policy.soft_delete_retention_days == 7 &&
      output.cache_policy.public_access_prevention == "enforced" &&
      output.cache_policy.uniform_bucket_level_access == true &&
      output.cache_policy.deletion_policy == "PREVENT"
    )
    error_message = "The Bazel backend must expose the reviewed private, versioned, rebuildable-cache contract."
  }

  assert {
    condition = (
      output.iam_contract.readers.role == "roles/storage.objectViewer" &&
      output.iam_contract.writers.role == "roles/storage.objectCreator" &&
      contains(output.iam_contract.readers.members, "serviceAccount:bazel-cache@mindclade-production.iam.gserviceaccount.com") &&
      contains(output.iam_contract.writers.members, "serviceAccount:bazel-cache@mindclade-production.iam.gserviceaccount.com") &&
      length(output.iam_contract.object_admin_members) == 0
    )
    error_message = "Bazel writers must be create-only readers without object-admin access."
  }

  assert {
    condition = (
      output.required_access_log_writer_grant.bucket == "mindclade-central-storage-logs" &&
      output.required_access_log_writer_grant.role == "roles/storage.objectCreator"
    )
    error_message = "The external access-log bucket grant must be returned to the caller."
  }
}

run "custom_bounded_cache_lifecycle" {
  command = plan

  variables {
    cache_ttl_days              = 30
    data_classification         = "confidential"
    noncurrent_version_ttl_days = 3
    soft_delete_retention_days  = 14
    retention_period_seconds    = 604800
  }

  assert {
    condition = (
      output.cache_policy.cache_ttl_days == 30 &&
      output.cache_policy.data_classification == "confidential" &&
      output.cache_policy.noncurrent_version_ttl_days == 3 &&
      output.cache_policy.soft_delete_retention_days == 14 &&
      output.cache_policy.retention_period_seconds == 604800
    )
    error_message = "Reviewed lifecycle overrides must remain visible in the policy output."
  }
}

run "reject_public_reader" {
  command = plan

  variables {
    reader_members = ["allUsers"]
  }

  expect_failures = [var.reader_members]
}

run "reject_malformed_principal_uri" {
  command = plan

  variables {
    reader_members = ["principalSet://iam.googleapis.com/projects/123/locations/global/workloadIdentityPools/pool/attribute.repository/bad value"]
  }

  expect_failures = [var.reader_members]
}

run "reject_public_data_classification" {
  command = plan

  variables {
    data_classification = "public"
  }

  expect_failures = [var.data_classification]
}

run "reject_public_writer" {
  command = plan

  variables {
    writer_members = ["allAuthenticatedUsers"]
  }

  expect_failures = [var.writer_members]
}

run "reject_direct_user_reader" {
  command = plan

  variables {
    reader_members = ["user:developer@example.com"]
  }

  expect_failures = [var.reader_members]
}

run "reject_group_writer" {
  command = plan

  variables {
    writer_members = ["group:developers@example.com"]
  }

  expect_failures = [var.writer_members]
}

run "reject_missing_writer" {
  command = plan

  variables {
    writer_members = []
  }

  expect_failures = [var.writer_members]
}

run "reject_cross_location_cmek" {
  command = plan

  variables {
    kms_key_name = "projects/mindclade-security/locations/us-east1/keyRings/build-cache/cryptoKeys/bazel"
  }

  expect_failures = [var.kms_key_name]
}

run "reject_retention_longer_than_cache_ttl" {
  command = plan

  variables {
    cache_ttl_days           = 1
    retention_period_seconds = 172800
  }

  expect_failures = [var.retention_period_seconds]
}

run "reject_self_logging" {
  command = plan

  variables {
    access_log_bucket = "mindclade-production-bazel-cache"
  }

  expect_failures = [var.access_log_bucket]
}
