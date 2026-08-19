# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "project_id" {
  description = "Created GCP project ID"
  value       = google_project.this.project_id
}

output "project_number" {
  description = "Created GCP project number"
  value       = google_project.this.number
}

output "project_name" {
  description = "Created GCP project name"
  value       = google_project.this.name
}

output "folder_id" {
  description = "Parent folder resource name"
  value       = google_project.this.folder_id
}

output "enabled_services" {
  description = "Google APIs managed for the project"
  value       = sort(tolist(local.enabled_services))
}

output "budget_name" {
  description = "Billing budget resource name, or null when budgets are disabled"
  value       = try(google_billing_budget.this[0].name, null)
}

output "shared_vpc_host_project_id" {
  description = "Shared VPC host project this project is attached to, or null when unattached"
  value       = var.shared_vpc_host_project_id
}
