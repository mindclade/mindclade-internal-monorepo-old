# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "project_ids" {
  description = "Project IDs keyed by the caller's stable project key."
  value       = { for key, project in module.project : key => project.project_id }
}

output "project_numbers" {
  description = "Project numbers keyed by the caller's stable project key."
  value       = { for key, project in module.project : key => project.project_number }
}

output "shared_vpc_host_project_id" {
  description = "Shared VPC host project ID, or null when this set owns no host."
  value       = local.network_host_id
}

output "monitoring_scope_project_id" {
  description = "Metrics scope host project ID, or null when this set owns no scope."
  value       = local.metrics_host_id
}
