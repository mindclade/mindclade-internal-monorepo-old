# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "nats" {
  description = "Cloud NAT gateways keyed by environment or another stable ownership key."
  type = map(object({
    project_id  = string
    network     = string
    region      = string
    router_name = string
    nat_name    = string

    nat_ip_allocate_option             = optional(string, "MANUAL_ONLY")
    static_ip_count                    = optional(number, 2)
    source_subnetwork_ip_ranges_to_nat = optional(string, "ALL_SUBNETWORKS_ALL_IP_RANGES")
    min_ports_per_vm                   = optional(number, 64)
    enable_dynamic_port_allocation     = optional(bool, true)
    max_ports_per_vm                   = optional(number, 512)
    tcp_established_idle_timeout_sec   = optional(number, 1200)
    tcp_transitory_idle_timeout_sec    = optional(number, 30)
    udp_idle_timeout_sec               = optional(number, 30)
    icmp_idle_timeout_sec              = optional(number, 30)
    log_config = optional(object({
      enable = optional(bool, true)
      filter = optional(string, "ERRORS_ONLY")
    }), {})
  }))

  validation {
    condition = length(var.nats) > 0 && alltrue([
      for _, nat in var.nats :
      can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", nat.project_id)) &&
      can(regex("^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/global/networks/[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", nat.network)) &&
      can(regex("^[a-z]+(?:-[a-z0-9]+)+[0-9]$", nat.region)) &&
      can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", nat.router_name)) &&
      can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", nat.nat_name)) &&
      contains(["AUTO_ONLY", "MANUAL_ONLY"], nat.nat_ip_allocate_option) &&
      (nat.nat_ip_allocate_option == "MANUAL_ONLY" ? nat.static_ip_count > 0 : nat.static_ip_count == 0) &&
      nat.min_ports_per_vm >= 32 && nat.max_ports_per_vm >= nat.min_ports_per_vm &&
      contains(["ALL_SUBNETWORKS_ALL_IP_RANGES", "ALL_SUBNETWORKS_ALL_PRIMARY_IP_RANGES"], nat.source_subnetwork_ip_ranges_to_nat) &&
      contains(["ERRORS_ONLY", "TRANSLATIONS_ONLY", "ALL"], nat.log_config.filter)
    ])
    error_message = "Each NAT requires exact project/network/region names, consistent IP allocation, safe port bounds, and a supported logging/source-range contract."
  }
}
