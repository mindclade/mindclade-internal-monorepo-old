output "node_pool_id" {
  description = "Fully qualified GKE node-pool identifier"
  value       = google_container_node_pool.this.id
}

output "node_pool_name" {
  description = "GKE GPU node-pool name"
  value       = google_container_node_pool.this.name
}

output "profile" {
  description = "NOVA accelerator profile configured by this node pool"
  value       = var.profile
}

output "machine_type" {
  description = "Compute Engine accelerator-optimized machine type"
  value       = local.selected_profile.machine_type
}

output "accelerator_type" {
  description = "GKE accelerator type"
  value       = local.selected_profile.accelerator_type
}

output "accelerator_count_per_node" {
  description = "Number of accelerators attached to each node"
  value       = local.selected_profile.accelerator_count
}

output "fabric" {
  description = "Expected accelerator fabric; live qualification remains required"
  value       = local.selected_profile.fabric
}

output "zone" {
  description = "Single zone configured for the accelerator node pool"
  value       = var.zone
}

output "capacity_mode" {
  description = "Configured accelerator capacity acquisition mode"
  value       = var.capacity_mode
}

output "managed_instance_group_urls" {
  description = "Managed instance groups backing the node pool"
  value       = google_container_node_pool.this.managed_instance_group_urls
}
