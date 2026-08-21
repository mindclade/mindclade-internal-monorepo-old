# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

variable "service_networking" {
  description = "Private Service Access ranges keyed by environment."
  type = map(object({
    project_id      = string
    network         = string
    allocated_range = string
  }))
  validation {
    condition = length(var.service_networking) > 0 && alltrue([
      for connection in values(var.service_networking) : can(cidrnetmask(connection.allocated_range))
    ])
    error_message = "Every service-networking connection requires an allocated CIDR."
  }
}
variable "google_api_endpoints" {
  description = "Optional consumer PSC endpoints for Google APIs."
  type = map(object({
    project_id = string
    region     = string
    network    = string
    subnetwork = string
    target     = string
    address    = string
  }))
  default = {}
}
variable "service_attachments" {
  description = "Producer service attachments admitted only with explicit targets, NAT subnets, and consumers."
  type = map(object({
    project_id            = string
    region                = string
    target_service        = string
    nat_subnets           = list(string)
    accepted_project_ids  = map(number)
    enable_proxy_protocol = optional(bool, false)
  }))
  default = {}
}
variable "labels" {
  type    = map(string)
  default = {}
}
