# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "worker_pool" {
  description = "Protected CPU worker pool contract"
  value       = module.worker_pool.node_pool
}

output "node_service_account" {
  description = "Dedicated node VM identity"
  value       = module.worker_pool.node_service_account
}

output "executor_service_account" {
  description = "Keyless executor workload identity"
  value = {
    email     = google_service_account.executor.email
    id        = google_service_account.executor.id
    name      = google_service_account.executor.name
    unique_id = google_service_account.executor.unique_id
  }
}

output "gitops_contract" {
  description = "Values the Kubernetes/GitOps owner must use when deploying the remote execution service"
  value = {
    namespace                  = var.kubernetes_namespace
    kubernetes_service_account = var.kubernetes_service_account
    gcp_service_account        = google_service_account.executor.email
    service_account_annotation = {
      "iam.gke.io/gcp-service-account" = google_service_account.executor.email
    }
    executor_image = var.executor_image
    cache_bucket   = var.cache_bucket_name
    node_selector = {
      "mindclade.dev/workload" = "bazel-remote-execution"
    }
    tolerations = [local.workload_taint]
  }
}

output "required_apis" {
  description = "APIs the project factory must enable before this module is applied"
  value = [
    "artifactregistry.googleapis.com",
    "container.googleapis.com",
    "iam.googleapis.com",
    "storage.googleapis.com",
  ]
}

output "project_iam_grants" {
  description = "Additive node and executor project grants created by this composition"
  value = {
    node     = sort(tolist(local.node_project_roles))
    executor = sort(tolist(local.workload_project_roles))
  }
}

output "qualification_requirements" {
  description = "Runtime evidence still required; Terraform cannot prove these conditions"
  value = [
    "pinned Nix worker closure matches executor image provenance",
    "Bazel execution platform and toolchain selection are deterministic",
    "CAS cold, warm, corrupt, and eviction behavior is tested",
    "worker cancellation and PodDisruptionBudget drain are bounded",
    "autoscaling backlog and scale-to-zero behavior meet SLO and cost targets",
    "spot interruption is retried without corrupting outputs when SPOT is enabled",
  ]
}
