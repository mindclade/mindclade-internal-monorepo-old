# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "router_names" {
  description = "Cloud Router names keyed by caller ownership key."
  value       = { for key, router in google_compute_router.nat : key => router.name }
}

output "nat_names" {
  description = "Cloud NAT names keyed by caller ownership key."
  value       = { for key, nat in google_compute_router_nat.nat : key => nat.name }
}

output "external_addresses" {
  description = "Stable external NAT addresses keyed by ownership key and ordinal."
  value       = { for key, address in google_compute_address.nat : key => address.address }
}
