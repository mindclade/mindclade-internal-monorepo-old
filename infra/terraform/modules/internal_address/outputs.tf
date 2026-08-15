# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "addresses" {
  description = <<-EOT
    The allocated IPv4 address by key.

    This is what the private DNS zones' A records point at. Reading it from here rather than
    hardcoding it is what keeps the record and the reservation from drifting apart.
  EOT
  value       = { for k, a in google_compute_address.this : k => a.address }
}

output "names" {
  description = <<-EOT
    The address resource name by key.

    THE Terraform↔Argo interface. A GKE Gateway refers to this string in
    `spec.addresses[].value` with `type: NamedAddress`; nothing generates it on the Argo side.
    Treat it as an interface: renaming it is a coordinated change across both repositories,
    and doing it in one alone leaves a Gateway that never receives an address, with no error
    beyond a status that never becomes programmed.
  EOT
  value       = { for k, a in google_compute_address.this : k => a.name }
}

output "self_links" {
  description = "Address self-links by key."
  value       = { for k, a in google_compute_address.this : k => a.self_link }
}
