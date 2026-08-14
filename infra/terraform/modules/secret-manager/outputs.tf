output "secret" {
  description = "Secret metadata only; payloads are deliberately absent"
  value = {
    id        = google_secret_manager_secret.this.id
    name      = google_secret_manager_secret.this.name
    secret_id = google_secret_manager_secret.this.secret_id
  }
}

output "replication_locations" {
  description = "Explicit replica locations, or an empty set for automatic replication"
  value       = toset(keys(var.user_managed_replicas))
}
