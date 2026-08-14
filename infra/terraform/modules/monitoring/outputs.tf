output "service_name" {
  description = "Fully qualified Cloud Monitoring custom-service resource name"
  value       = google_monitoring_custom_service.this.name
}

output "service_id" {
  description = "Cloud Monitoring custom-service ID"
  value       = google_monitoring_custom_service.this.service_id
}

output "slo_names" {
  description = "Fully qualified Cloud Monitoring SLO resource names by objective ID"
  value       = { for slo_id, slo in google_monitoring_slo.this : slo_id => slo.name }
}

output "fast_burn_alert_policy_names" {
  description = "Fast-burn alert-policy resource names by objective ID"
  value       = { for slo_id, policy in google_monitoring_alert_policy.fast_burn : slo_id => policy.name }
}

output "slow_burn_alert_policy_names" {
  description = "Sustained-burn alert-policy resource names by objective ID"
  value       = { for slo_id, policy in google_monitoring_alert_policy.slow_burn : slo_id => policy.name }
}

output "dashboard_name" {
  description = "Cloud Monitoring dashboard resource name"
  value       = google_monitoring_dashboard.this.id
}

output "runbook_url" {
  description = "Responder runbook linked from the module's alerts and dashboard"
  value       = var.runbook_url
}

output "slo_contracts" {
  description = "Reviewable goals, windows, and burn thresholds without metric-filter payloads"
  value = {
    for slo_id, slo in var.slos : slo_id => {
      goal                = slo.goal
      rolling_period_days = slo.rolling_period_days
      fast_burn           = slo.fast_burn
      slow_burn           = slo.slow_burn
    }
  }
}
