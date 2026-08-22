# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

output "certificate_names" {
  description = "Stable Certificate Manager resource names by attachment key for networking.gke.io/cert-manager-certs."
  value       = { for key, certificate in google_certificate_manager_certificate.this : key => certificate.name }
}

output "certificate_ids" {
  description = "Fully qualified regional Certificate Manager certificate ids by attachment key."
  value       = { for key, certificate in google_certificate_manager_certificate.this : key => certificate.id }
}

output "dns_authorization_records" {
  description = "Generated public CNAME records by authorization key, suitable for read-only evidence and temporary incumbent-DNS mirroring."
  value = {
    for key, authorization in google_certificate_manager_dns_authorization.this : key => {
      name         = authorization.dns_resource_record[0].name
      type         = authorization.dns_resource_record[0].type
      data         = authorization.dns_resource_record[0].data
      ttl          = var.dns_authorizations[key].ttl
      managed_zone = var.dns_authorizations[key].managed_zone
    }
  }
}
