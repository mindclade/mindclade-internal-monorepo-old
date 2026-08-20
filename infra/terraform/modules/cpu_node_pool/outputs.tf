# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "node_pool" {
  description = "Created GKE CPU node-pool identity and backing managed instance groups"
  value = {
    id                          = google_container_node_pool.this.id
    name                        = google_container_node_pool.this.name
    location                    = google_container_node_pool.this.location
    managed_instance_group_urls = google_container_node_pool.this.managed_instance_group_urls
  }
}

output "node_service_account" {
  description = "Dedicated keyless VM service account created for the node pool"
  value = {
    id        = google_service_account.nodes.id
    email     = google_service_account.nodes.email
    name      = google_service_account.nodes.name
    unique_id = google_service_account.nodes.unique_id
  }
}

output "profile" {
  description = "Configured workload profile"
  value       = var.profile
}

output "machine_type" {
  description = "Effective Compute Engine machine type"
  value       = local.machine_type
}

output "capacity_type" {
  description = "Configured ON_DEMAND or SPOT capacity type"
  value       = var.capacity_type
}

output "required_node_service_account_project_roles" {
  description = "Additive project roles this module grants to the dedicated node service account; image-project Artifact Registry access remains separately scoped"
  value       = local.required_node_service_account_project_roles
}

output "node_labels" {
  description = "Effective Kubernetes node labels, including module-owned identity labels"
  value       = local.node_labels
}

output "node_taints" {
  description = "Effective Kubernetes taints, including module-owned high-memory and Spot isolation"
  value       = local.node_taints
}
