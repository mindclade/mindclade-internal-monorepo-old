# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

variables {
  project_id               = "mindclade-build"
  cluster_name             = "mindclade-platform"
  region                   = "us-central1"
  node_locations           = ["us-central1-a", "us-central1-b"]
  pod_secondary_range_name = "gke-pods"
  node_service_account_id  = "bazel-executor-nodes"
  executor_image           = "us-central1-docker.pkg.dev/mindclade-build/workers/bazel@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  cache_bucket_name        = "mindclade-bazel-cas"
  environment              = "production"
  owner                    = "release-engineering"
}

run "keyless_protected_executor_contract" {
  command = plan

  assert {
    condition = (
      google_service_account.executor.deletion_policy == "PREVENT" &&
      google_service_account_iam_member.gke_workload_identity.role == "roles/iam.workloadIdentityUser" &&
      endswith(output.gitops_contract.executor_image, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
    )
    error_message = "The executor identity must be protected, keyless, and tied to an immutable image."
  }

  assert {
    condition = (
      google_storage_bucket_iam_member.cache_creator.role == "roles/storage.objectCreator" &&
      google_storage_bucket_iam_member.cache_viewer.role == "roles/storage.objectViewer"
    )
    error_message = "The executor may append to and read the cache without bucket administration."
  }
}

run "reject_tagged_executor_image" {
  command = plan

  variables {
    executor_image = "us-central1-docker.pkg.dev/mindclade-build/workers/bazel:latest"
  }

  expect_failures = [var.executor_image]
}

run "reject_admin_role" {
  command = plan

  variables {
    executor_project_roles = ["roles/storage.admin"]
  }

  expect_failures = [var.executor_project_roles]
}

run "reject_unacknowledged_spot" {
  command = plan

  variables {
    capacity_type   = "SPOT"
    total_min_nodes = 0
  }

  expect_failures = [var.spot_approval]
}
