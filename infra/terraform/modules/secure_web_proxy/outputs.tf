# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "gateway_ids" {
  description = "Immutable Secure Web Proxy resource identifiers keyed by ownership key."
  value       = { for key, gateway in google_network_services_gateway.proxy : key => gateway.id }
}

output "https_proxy_urls" {
  description = "HTTPS proxy URLs for workload configuration; certificate trust is a separate secret contract."
  value       = { for key, proxy in local.proxies : key => "https://${proxy.address}:443" }
}

output "allowed_hosts" {
  description = "Normalized exact host allowlists keyed by ownership key for release evidence."
  value       = { for key, proxy in local.proxies : key => proxy.allowed_hosts }
}
