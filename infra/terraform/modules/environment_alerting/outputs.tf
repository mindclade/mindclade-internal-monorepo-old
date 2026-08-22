# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "notification_channel_names" { value = { for key, channel in google_monitoring_notification_channel.this : key => channel.name } }
output "alert_policy_names" { value = { for key, policy in google_monitoring_alert_policy.this : key => policy.name } }
output "metrics_scope_contract" {
  description = "Existing scope read by these policies; project_factory remains the sole membership owner."
  value       = var.metrics_scope_project
}
