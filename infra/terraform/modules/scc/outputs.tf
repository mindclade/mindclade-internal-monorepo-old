# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "notification_topic_ids" {
  description = "Pub/Sub topic ids keyed by notification name. Anything wanting findings subscribes to one of these rather than polling SCC."
  value       = { for name, topic in google_pubsub_topic.findings : name => topic.id }
}

output "notification_config_ids" {
  description = "SCC notification config resource ids keyed by name."
  value       = { for name, config in google_scc_notification_config.this : name => config.id }
}

output "findings_dataset_id" {
  description = "BigQuery dataset holding exported findings, or null when no export is configured."
  value       = var.bigquery_export == null ? null : google_bigquery_dataset.findings[0].id
}

output "enabled_services" {
  description = "Detectors explicitly enabled. Surfaced so a reviewer can diff what is running against what was last approved, rather than reading a tier default that moves."
  value       = sort([for name, state in var.services : name if state == "ENABLE"])
}

output "service_enablement_commands" {
  description = <<-EOT
    The gcloud invocations that actually enable or disable each detector.

    No Terraform resource exists for built-in SCC service enablement, so this is the honest
    interface: the module records what was intended and hands back the commands, rather than
    reporting a green apply for something it never configured. Run these once, then let the
    drift sweep compare `gcloud scc manage services list` against `enabled_services`.
  EOT
  value = [
    for name, state in var.services :
    "gcloud scc manage services update ${name} --organization=${var.org_id} --enablement-state=${lower(state)}d"
  ]
}

output "disabled_services" {
  description = "Detectors explicitly turned off. A short list that should stay short."
  value       = sort([for name, state in var.services : name if state == "DISABLE"])
}

output "mute_config_ids" {
  description = "Standing mute ids keyed by name."
  value       = { for name, mute in google_scc_mute_config.this : name => mute.id }
}

output "mute_reasons" {
  description = "Why each mute exists, keyed by name. In state and in the plan diff, so a mute that is still present a year later is visible without opening the console."
  value       = { for name, cfg in var.mute_configs : name => trimspace(cfg.description) }
}
