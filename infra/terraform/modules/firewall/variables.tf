# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "firewalls" {
  description = "Classic VPC firewall policies keyed by environment or another stable owner."
  type = map(object({
    project_id                  = string
    network                     = string
    enable_logging_on_deny_only = optional(bool, true)
    rules = map(object({
      direction          = string
      priority           = number
      action             = string
      description        = optional(string, "Managed VPC firewall rule.")
      disabled           = optional(bool, false)
      source_ranges      = optional(set(string), [])
      destination_ranges = optional(set(string), [])
      source_tags        = optional(set(string), [])
      target_tags        = optional(set(string), [])
      allow = optional(list(object({
        protocol = string
        ports    = optional(set(string), [])
      })), [])
      deny = optional(list(object({
        protocol = string
        ports    = optional(set(string), [])
      })), [])
      log_config = optional(object({
        metadata = optional(string, "INCLUDE_ALL_METADATA")
      }))
    }))
  }))

  validation {
    condition = length(var.firewalls) > 0 && alltrue(flatten([
      for _, firewall in var.firewalls : [
        for name, rule in firewall.rules :
        can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", name)) &&
        contains(["INGRESS", "EGRESS"], rule.direction) &&
        contains(["allow", "deny"], rule.action) &&
        rule.priority >= 0 && rule.priority <= 65535 &&
        (rule.action == "allow" ? length(rule.allow) > 0 && length(rule.deny) == 0 : length(rule.deny) > 0 && length(rule.allow) == 0) &&
        (rule.direction == "INGRESS" ? length(rule.destination_ranges) == 0 : length(rule.source_ranges) == 0 && length(rule.source_tags) == 0) &&
        alltrue([for item in concat(rule.allow, rule.deny) : length(trimspace(item.protocol)) > 0])
      ]
    ]))
    error_message = "Firewall rules require an exact direction/action, one allow-or-deny block, valid priority, and direction-appropriate ranges."
  }

  validation {
    condition = alltrue(flatten([
      for _, firewall in var.firewalls : [
        for _, rule in firewall.rules :
        rule.log_config == null || contains(["EXCLUDE_ALL_METADATA", "INCLUDE_ALL_METADATA"], rule.log_config.metadata)
      ]
    ]))
    error_message = "Firewall log metadata must use a provider-supported exact value."
  }
}
