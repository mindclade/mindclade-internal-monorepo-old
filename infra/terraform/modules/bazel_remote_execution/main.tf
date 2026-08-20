# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  node_project_roles = toset([
    "roles/artifactregistry.reader",
  ])

  workload_project_roles = var.executor_project_roles
  executor_member        = "serviceAccount:${google_service_account.executor.email}"

  workload_taint = {
    key    = "mindclade.dev/workload"
    value  = "bazel-remote-execution"
    effect = "NO_SCHEDULE"
  }
}

module "worker_pool" {
  source = "../cpu_node_pool"

  project_id                   = var.project_id
  cluster_name                 = var.cluster_name
  name                         = var.node_pool_name
  region                       = var.region
  node_locations               = var.node_locations
  pod_secondary_range_name     = var.pod_secondary_range_name
  service_account_id           = var.node_service_account_id
  service_account_display_name = "Bazel remote execution node identity"
  environment                  = var.environment
  owner                        = var.owner

  profile           = var.profile
  machine_type      = var.machine_type
  capacity_type     = var.capacity_type
  spot_approval     = var.spot_approval
  total_min_nodes   = var.total_min_nodes
  total_max_nodes   = var.total_max_nodes
  max_pods_per_node = var.max_pods_per_node

  boot_disk_type    = var.boot_disk_type
  boot_disk_size_gb = var.boot_disk_size_gb
  boot_disk_kms_key = var.boot_disk_kms_key

  resource_labels = var.resource_labels
  node_labels = merge(var.node_labels, {
    "mindclade.dev/workload" = "bazel-remote-execution"
  })
  additional_taints = concat(var.additional_taints, [local.workload_taint])

  upgrade_max_surge       = var.upgrade_max_surge
  upgrade_max_unavailable = var.upgrade_max_unavailable
  node_drain_grace_period = var.node_drain_grace_period
  node_drain_pdb_timeout  = var.node_drain_pdb_timeout
}

resource "google_service_account" "executor" {
  project      = var.project_id
  account_id   = var.executor_service_account_id
  display_name = "Bazel remote execution workload identity"
  description  = "Keyless GKE workload identity used only by the Bazel remote execution service."

  deletion_policy = "PREVENT"

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = var.node_service_account_id != var.executor_service_account_id
      error_message = "Node and executor workload service accounts must be distinct identities."
    }

    precondition {
      condition     = var.total_min_nodes <= var.total_max_nodes
      error_message = "total_min_nodes must not exceed total_max_nodes."
    }

    precondition {
      condition     = var.capacity_type != "SPOT" || var.spot_approval == "I ACCEPT EVICTION AND CAPACITY-LOSS RISK"
      error_message = "SPOT capacity requires the exact interruption acknowledgement."
    }

    precondition {
      condition     = var.capacity_type != "SPOT" || var.total_min_nodes == 0
      error_message = "SPOT remote-execution pools must allow scale-to-zero with total_min_nodes = 0."
    }
  }
}

# GKE node identities need these narrow project roles. Member resources are additive and do
# not replace principals managed by another state.
resource "google_project_iam_member" "node" {
  for_each = local.node_project_roles

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${module.worker_pool.node_service_account.email}"
}

resource "google_project_iam_member" "executor" {
  for_each = local.workload_project_roles

  project = var.project_id
  role    = each.value
  member  = local.executor_member
}

resource "google_service_account_iam_member" "gke_workload_identity" {
  service_account_id = google_service_account.executor.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project_id}.svc.id.goog[${var.kubernetes_namespace}/${var.kubernetes_service_account}]"
}

# CAS/Action Cache objects are append-only from this identity: it can create and read but not
# administer the bucket or lifecycle. The cache module owns expiry and emergency deletion.
resource "google_storage_bucket_iam_member" "cache_creator" {
  bucket = var.cache_bucket_name
  role   = "roles/storage.objectCreator"
  member = local.executor_member
}

resource "google_storage_bucket_iam_member" "cache_viewer" {
  bucket = var.cache_bucket_name
  role   = "roles/storage.objectViewer"
  member = local.executor_member
}
