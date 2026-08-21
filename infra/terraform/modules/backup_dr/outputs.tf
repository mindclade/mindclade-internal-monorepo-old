# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

output "backup_plan" { value = google_gke_backup_backup_plan.this.name }
output "replica_buckets" { value = { for key, bucket in google_storage_bucket.replica : key => bucket.name } }
output "transfer_jobs" { value = { for key, job in google_storage_transfer_job.replica : key => job.name } }
output "restore_exclusion_contract" {
  description = "Namespaces that protected restore automation must omit; the provider backup API has no exclusion field."
  value       = var.gke_backup.excluded_namespaces
}
