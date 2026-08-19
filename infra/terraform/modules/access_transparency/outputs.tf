# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "bucket_name" {
  description = "Archive bucket holding the Access Transparency records."
  value       = google_storage_bucket.access_transparency.name
}

output "sink_id" {
  description = "Organization sink resource id."
  value       = google_logging_organization_sink.access_transparency.id
}

output "writer_identity" {
  description = "Service account the sink writes as. Check this against the bucket's IAM policy when entries stop arriving — a sink whose writer lacks permission reports healthy and delivers nothing."
  value       = google_logging_organization_sink.access_transparency.writer_identity
}

output "retention_days" {
  description = "How long a record cannot be deleted for, in days."
  value       = coalesce(var.sink.bucket.retention_days, 2555)
}

output "alert_policy_id" {
  description = "Alert policy resource id, or null when no alert is configured."
  value       = var.alert == null ? null : google_monitoring_alert_policy.access[0].id
}

output "notification_channel_ids" {
  description = "Monitoring notification channel ids keyed by email address."
  value       = { for email, channel in google_monitoring_notification_channel.this : email => channel.id }
}
