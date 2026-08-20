# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "reserved_range_name" {
  description = "Name of the reserved range; pass to Cloud SQL allocated_ip_range and Memorystore reserved_ip_range consumers"
  value       = google_compute_global_address.reserved.name
}

output "reserved_range_cidr" {
  description = "Reserved managed-services block in CIDR notation"
  value       = "${google_compute_global_address.reserved.address}/${google_compute_global_address.reserved.prefix_length}"
}

output "connection_id" {
  description = "Identifier of the service networking connection"
  value       = google_service_networking_connection.this.id
}

output "peering_name" {
  description = "Name of the VPC peering created by the service networking connection"
  value       = google_service_networking_connection.this.peering
}
