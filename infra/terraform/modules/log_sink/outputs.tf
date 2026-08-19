# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "sink_ids" {
  description = "Organization sink resource ids keyed by sink name."
  value = merge(
    { for name, sink in google_logging_organization_sink.logging : name => sink.id },
    { for name, sink in google_logging_organization_sink.storage : name => sink.id },
  )
}

output "writer_identities" {
  description = <<-EOT
    Service account each sink writes as, keyed by sink name. Surfaced because a sink whose
    writer lacks permission on its destination reports healthy and delivers nothing — this
    is the value to check against the destination's IAM policy when entries stop arriving.
  EOT
  value = merge(
    { for name, sink in google_logging_organization_sink.logging : name => sink.writer_identity },
    { for name, sink in google_logging_organization_sink.storage : name => sink.writer_identity },
  )
}

output "log_bucket_ids" {
  description = "Cloud Logging bucket ids keyed by sink name."
  value       = { for name, bucket in google_logging_project_bucket_config.this : name => bucket.id }
}

output "storage_bucket_names" {
  description = "GCS archive bucket names keyed by sink name."
  value       = { for name, bucket in google_storage_bucket.this : name => bucket.name }
}

output "analytics_enabled_buckets" {
  description = "Sinks whose log bucket has Log Analytics on. Irreversible after creation, so this records what was chosen at the only moment it could be."
  value       = sort([for name, sink in local.logging_sinks : name if sink.enable_analytics])
}
