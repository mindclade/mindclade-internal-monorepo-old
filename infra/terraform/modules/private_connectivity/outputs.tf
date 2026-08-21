# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

output "service_networking_ranges" {
  value       = { for key, address in google_compute_global_address.service_networking : key => address.name }
  description = "Reserved service-networking range names keyed by environment."
}
output "service_attachment_self_links" {
  value = { for key, attachment in google_compute_service_attachment.this : key => attachment.self_link }
}
output "google_api_forwarding_rules" {
  value = { for key, rule in google_compute_forwarding_rule.google_api : key => rule.self_link }
}
