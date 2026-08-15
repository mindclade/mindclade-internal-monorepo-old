# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "addresses" {
  description = <<-EOT
    Reserved internal IPv4 addresses, keyed by a stable name — in this estate, the
    environment.

    The map KEY is not the resource name; `name` is, and it is what a Kubernetes Gateway
    refers to in `spec.addresses[].value`. That name is an interface between Terraform and
    Argo: Terraform reserves the address, the Gateway names it, and neither generates it.
    Renaming one without the other produces a Gateway that never gets an address, and the
    only symptom is a Gateway stuck without a programmed status.
  EOT

  type = map(object({
    project_id = string
    name       = string
    region     = string

    # Self-link of the subnet to allocate from. It must be a PRIVATE subnet: a
    # REGIONAL_MANAGED_PROXY subnet holds the load balancer's proxies and cannot allocate an
    # address, which is the natural mistake here because both belong to the same load
    # balancer.
    subnetwork = string

    # Omit to let GCP choose. Pinning it is worth doing once a DNS record points at it, since
    # an unpinned address can come back different after a destroy/recreate — and the DNS
    # records in the private zones would then point at nothing.
    address = optional(string)

    description = optional(string, "Reserved internal address managed by Terraform.")
    purpose     = optional(string, "GCE_ENDPOINT")
  }))

  validation {
    condition = alltrue([
      for key, addr in var.addresses :
      can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", addr.name)) &&
      can(regex("^[a-z]+(?:-[a-z0-9]+)+[0-9]$", addr.region)) &&
      length(trimspace(addr.subnetwork)) > 0
    ])
    error_message = "Each address requires an RFC1035 name, a region, and a subnetwork self-link."
  }

  validation {
    condition = alltrue([
      for key, addr in var.addresses :
      addr.address == null || can(cidrnetmask("${addr.address}/32"))
    ])
    error_message = "address, when set, must be a literal IPv4 address inside the subnetwork's range."
  }

  validation {
    condition = alltrue([
      for key, addr in var.addresses :
      contains(["GCE_ENDPOINT", "SHARED_LOADBALANCER_VIP"], addr.purpose)
    ])
    error_message = "purpose must be GCE_ENDPOINT or SHARED_LOADBALANCER_VIP for an internal address allocated from a subnet."
  }
}
