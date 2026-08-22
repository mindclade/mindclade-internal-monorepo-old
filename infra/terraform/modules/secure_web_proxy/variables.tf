# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "proxies" {
  description = "TLS-inspecting explicit Secure Web Proxies keyed by stable environment ownership key."
  type = map(object({
    project_id                = string
    region                    = string
    name                      = string
    scope                     = string
    address                   = string
    network                   = string
    subnetwork                = string
    gateway_certificate_url   = string
    tls_inspection_policy_url = string
    allowed_hosts             = set(string)
  }))

  validation {
    condition = length(var.proxies) > 0 && length(var.proxies) <= 8 && alltrue([
      for _, proxy in var.proxies :
      can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", proxy.project_id)) &&
      can(regex("^[a-z]+(?:-[a-z0-9]+)+[0-9]$", proxy.region)) &&
      can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", proxy.name)) &&
      can(regex("^[a-z](?:[-a-z0-9]{0,62})?$", proxy.scope)) &&
      can(cidrhost("${proxy.address}/32", 0)) &&
      can(regex("^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/global/networks/[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", proxy.network)) &&
      can(regex("^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/regions/${proxy.region}/subnetworks/[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", proxy.subnetwork)) &&
      can(regex("^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/locations/${proxy.region}/certificates/[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", proxy.gateway_certificate_url)) &&
      can(regex("^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/locations/${proxy.region}/tlsInspectionPolicies/[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", proxy.tls_inspection_policy_url)) &&
      length(proxy.allowed_hosts) > 0 && length(proxy.allowed_hosts) <= 32 && alltrue([
        for host in proxy.allowed_hosts :
        length(host) <= 253 && can(regex("^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])$", host)) && !strcontains(host, "..")
      ])
    ])
    error_message = "Each proxy requires exact regional resource URLs, one IPv4 address, safe names, and 1-32 exact lowercase provider hostnames; wildcards and CEL fragments are rejected."
  }
}
