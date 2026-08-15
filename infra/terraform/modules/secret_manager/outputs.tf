output "secret_ids" {
  description = "Secret id by map key."
  value       = { for k, s in google_secret_manager_secret.this : k => s.secret_id }
}

output "secret_names" {
  description = "Fully qualified secret names, for a CSI SecretProviderClass or a workload's own config."
  value       = { for k, s in google_secret_manager_secret.this : k => s.name }
}

output "rotating_secret_ids" {
  description = <<-EOT
    Secrets carrying a rotation period.

    Exported so a rotation job can enumerate what it owns rather than being configured with a
    list that drifts from this one.
  EOT
  value       = [for k, s in var.secrets : k if s.rotation_period != null]
}
