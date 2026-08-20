# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

variables {
  project_id        = "mindclade-production"
  bucket_name       = "mindclade-production-nix-cache"
  location          = "us-central1"
  kms_key_name      = "projects/mindclade-security/locations/us-central1/keyRings/build-cache/cryptoKeys/nix"
  access_log_bucket = "mindclade-central-storage-logs"
  environment       = "production"
  owner             = "developer-platform"
  reader_members = [
    "principalSet://iam.googleapis.com/projects/123456789/locations/global/workloadIdentityPools/mindclade-production.svc.id.goog/namespace/workloads",
  ]
  writer_members = [
    "serviceAccount:nix-publisher@mindclade-production.iam.gserviceaccount.com",
  ]
}

run "immutable_binary_cache_contract" {
  command = plan

  assert {
    condition = (
      output.substituter_uri == "https://storage.googleapis.com/mindclade-production-nix-cache" &&
      output.immutable_policy.data_lifecycle == "immutable" &&
      output.immutable_policy.data_classification == "internal" &&
      output.immutable_policy.versioning_enabled == true &&
      output.immutable_policy.create_only_publishers == true &&
      output.immutable_policy.object_delete_rules == 0 &&
      output.immutable_policy.soft_delete_retention_days == 30 &&
      output.immutable_policy.public_access_prevention == "enforced" &&
      output.immutable_policy.deletion_policy == "PREVENT"
    )
    error_message = "The Nix backend must expose the reviewed private, versioned, immutable-publication contract."
  }

  assert {
    condition = (
      output.iam_contract.readers.role == "roles/storage.objectViewer" &&
      output.iam_contract.publishers.role == "roles/storage.objectCreator" &&
      contains(output.iam_contract.readers.members, "serviceAccount:nix-publisher@mindclade-production.iam.gserviceaccount.com") &&
      contains(output.iam_contract.publishers.members, "serviceAccount:nix-publisher@mindclade-production.iam.gserviceaccount.com") &&
      length(output.iam_contract.object_admin_members) == 0
    )
    error_message = "Publishers must be create-only readers without object-admin access."
  }

  assert {
    condition = (
      output.required_access_log_writer_grant.bucket == "mindclade-central-storage-logs" &&
      output.required_access_log_writer_grant.member == "group:cloud-storage-analytics@google.com"
    )
    error_message = "The external access-log bucket grant must be returned to the caller."
  }
}

run "explicitly_locked_retention_contract" {
  command = plan

  variables {
    data_classification         = "restricted"
    lock_retention_policy       = true
    retention_lock_confirmation = "LOCKING A CLOUD STORAGE RETENTION POLICY IS IRREVERSIBLE"
  }

  assert {
    condition     = output.immutable_policy.retention_policy_locked == true && output.immutable_policy.data_classification == "restricted"
    error_message = "An explicitly approved retention lock must be visible in the policy contract."
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

run "reject_public_publisher" {
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

run "reject_group_publisher" {
  command = plan

  variables {
    writer_members = ["group:publishers@example.com"]
  }

  expect_failures = [var.writer_members]
}

run "reject_missing_publisher" {
  command = plan

  variables {
    writer_members = []
  }

  expect_failures = [var.writer_members]
}

run "reject_cross_location_cmek" {
  command = plan

  variables {
    kms_key_name = "projects/mindclade-security/locations/us-east1/keyRings/build-cache/cryptoKeys/nix"
  }

  expect_failures = [var.kms_key_name]
}

run "reject_too_short_retention" {
  command = plan

  variables {
    retention_period_seconds = 86400
  }

  expect_failures = [var.retention_period_seconds]
}

run "reject_retention_lock_without_exact_confirmation" {
  command = plan

  variables {
    lock_retention_policy = true
  }

  expect_failures = [var.retention_lock_confirmation]
}

run "reject_confirmation_when_retention_is_not_locked" {
  command = plan

  variables {
    retention_lock_confirmation = "LOCKING A CLOUD STORAGE RETENTION POLICY IS IRREVERSIBLE"
  }

  expect_failures = [var.retention_lock_confirmation]
}

run "reject_self_logging" {
  command = plan

  variables {
    access_log_bucket = "mindclade-production-nix-cache"
  }

  expect_failures = [var.access_log_bucket]
}
