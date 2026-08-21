# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

variable "org_id" { type = string }
variable "policy_name" { type = string }
variable "perimeter" {
  type = object({
    name                      = string
    title                     = string
    use_explicit_dry_run_spec = bool
    resources                 = set(string)
    restricted_services       = set(string)
    vpc_accessible_services = object({
      enable_restriction = bool
      allowed_services   = set(string)
    })
  })
  validation {
    condition     = var.perimeter.use_explicit_dry_run_spec
    error_message = "A new perimeter must remain in explicit dry-run until connected violation-log evidence is approved."
  }
}
variable "ingress_policies" {
  type = list(object({
    title = string
    from = object({
      identities           = set(string)
      identity_type        = optional(string)
      source_access_levels = set(string)
    })
    to = object({
      resources = set(string)
      operations = map(object({
        methods = set(string)
      }))
    })
  }))
}
variable "egress_policies" {
  description = "Egress remains closed for the initial qualified perimeter release."
  type        = list(object({ title = string }))
  default     = []
  validation {
    condition     = length(var.egress_policies) == 0
    error_message = "Egress policies require a dedicated typed contract and exfiltration review."
  }
}
variable "access_levels" {
  description = "IP-based access levels are forbidden by the initial identity-only perimeter contract."
  type        = map(object({ title = string }))
  default     = {}
  validation {
    condition     = length(var.access_levels) == 0
    error_message = "Access levels are intentionally empty; identity-scoped ingress is required."
  }
}
