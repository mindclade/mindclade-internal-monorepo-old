output "cluster_id" {
  description = "Fully qualified GKE cluster identifier"
  value       = google_container_cluster.this.id
}

output "cluster_name" {
  description = "GKE cluster name"
  value       = google_container_cluster.this.name
}

output "cluster_location" {
  description = "GKE regional control-plane location"
  value       = google_container_cluster.this.location
}

output "cluster_endpoint" {
  description = "Private Kubernetes API endpoint; treat as sensitive network metadata"
  value       = google_container_cluster.this.endpoint
  sensitive   = true
}

output "workload_identity_pool" {
  description = "Workload Identity Federation pool configured for Kubernetes service accounts"
  value       = "${var.project_id}.svc.id.goog"
}

output "system_node_pool_name" {
  description = "Managed non-accelerator system node-pool name"
  value       = google_container_node_pool.system.name
}

output "network" {
  description = "VPC network attached to the cluster"
  value       = google_container_cluster.this.network
}

output "subnetwork" {
  description = "VPC subnetwork attached to the cluster"
  value       = google_container_cluster.this.subnetwork
}
