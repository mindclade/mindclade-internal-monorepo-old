# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "metrics_scope_name" {
  description = "Fully qualified metrics-scope resource name"
  value       = local.metrics_scope_name
}

output "monitored_projects" {
  description = "Protected metrics-scope memberships keyed by monitored project ID"
  value = {
    for project_id, membership in google_monitoring_monitored_project.this : project_id => {
      id            = membership.id
      project_id    = project_id
      metrics_scope = membership.metrics_scope
    }
  }
}

output "service_names" {
  description = "Fully qualified custom-service resource names keyed by service ID"
  value       = { for service_id, service in module.service : service_id => service.service_name }
}

output "slo_names" {
  description = "SLO resource names keyed first by service ID and then objective ID"
  value       = { for service_id, service in module.service : service_id => service.slo_names }
}

output "fast_burn_alert_policy_names" {
  description = "Fast-burn alert-policy names keyed first by service ID and then objective ID"
  value       = { for service_id, service in module.service : service_id => service.fast_burn_alert_policy_names }
}

output "slow_burn_alert_policy_names" {
  description = "Sustained-burn alert-policy names keyed first by service ID and then objective ID"
  value       = { for service_id, service in module.service : service_id => service.slow_burn_alert_policy_names }
}

output "dashboard_names" {
  description = "Protected Monitoring dashboard names keyed by service ID"
  value       = { for service_id, service in module.service : service_id => service.dashboard_name }
}

output "runbook_urls" {
  description = "Responder runbooks keyed by service ID"
  value       = { for service_id, service in module.service : service_id => service.runbook_url }
}

output "slo_contracts" {
  description = "Reviewable goals, windows, and burn thresholds keyed by service and objective"
  value       = { for service_id, service in module.service : service_id => service.slo_contracts }
}
