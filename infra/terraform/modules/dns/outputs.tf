# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

output "zone_names" {
  description = "Cloud DNS zone name by map key."
  value       = { for k, z in google_dns_managed_zone.this : k => z.name }
}

output "zone_ids" {
  description = "Fully qualified zone ids by map key."
  value       = { for k, z in google_dns_managed_zone.this : k => z.id }
}

output "name_servers" {
  description = <<-EOT
    Delegation name servers by map key, for public zones.

    These are what the registrar has to be pointed at. A public zone whose registrar still
    delegates elsewhere validates nothing and issues no certificate, and the ACME failure
    names the challenge rather than the delegation.
  EOT
  value       = { for k, z in google_dns_managed_zone.this : k => z.name_servers }
}

output "inbound_policy_name" {
  description = <<-EOT
    Name of the inbound server policy, or null when inbound forwarding is disabled.

    The forwarding target ADDRESSES are deliberately not an output: Cloud DNS allocates one
    per attached network, and the Terraform provider does not expose them on this resource.
    Read them back with

      gcloud compute addresses list --filter='purpose=DNS_RESOLVER' \
        --format='table(address, subnetwork, region)'

    then configure the on-prem or VPN resolver with a conditional forwarder pointing at them
    for each private domain. That step is outside Terraform, which is exactly why it is the
    one most often skipped — and skipping it produces names that resolve in-cluster and
    NXDOMAIN on a laptop, with nothing in this state file to suggest why.
  EOT
  value       = try(google_dns_policy.inbound[0].name, null)
}
