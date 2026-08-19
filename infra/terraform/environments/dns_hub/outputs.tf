# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "name_servers" {
  description = <<-EOT
    Delegation name servers per domain. THIS IS THE POINT OF THE APPLY.

    Every one of these has to be entered at the registrar before anything else
    in the estate works. Until the registrar delegates here, this zone is a
    correct set of records that no resolver on the internet consults, and the
    first symptom is an ACME challenge that times out rather than one that is
    refused -- which reads as a cert-manager problem.

    Print them with:  terraform output -json name_servers
  EOT
  value       = module.dns.name_servers
}

output "zone_names" {
  description = <<-EOT
    Cloud DNS zone name per domain, for `gcloud dns record-sets list --zone=...`
    and for the cert-manager ClusterIssuer's DNS-01 solver configuration.
  EOT
  value       = module.dns.zone_names
}

output "project_id" {
  description = <<-EOT
    Project holding the zones. cert-manager's DNS-01 solver needs it, and its
    workload identity binding grants roles/dns.admin here rather than in the
    cluster's own project.
  EOT
  value       = var.project_id
}

output "delegation_check" {
  description = <<-EOT
    Ready-to-paste command per domain that asks the PUBLIC internet, not Cloud
    DNS, who is authoritative.

    Checking with `gcloud dns` instead only proves Terraform applied. This is
    the one that distinguishes "the zone exists" from "the zone is live", and
    they are days apart when a registrar change is still propagating.
  EOT
  value = {
    for key, domain in var.domains :
    key => "dig +short NS ${trimsuffix(domain.dns_name, ".")} @1.1.1.1"
  }
}

output "caa_check" {
  description = <<-EOT
    Per-domain CAA verification. Should list only the CAs in
    var.certificate_authorities; on apex-only domains it should also show
    `issuewild ";"`, which is the record that forbids wildcard issuance.
  EOT
  value = {
    for key, domain in var.domains :
    key => "dig +short CAA ${trimsuffix(domain.dns_name, ".")} @1.1.1.1"
  }
}
