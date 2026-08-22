# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" { type = string }
variable "network" { type = string }
variable "region" { type = string }
variable "interconnects" {
  type = map(object({
    name                 = string
    location             = string
    description          = string
    link_type            = string
    requested_link_count = number
  }))
  validation {
    condition     = length(var.interconnects) >= 2 && length(distinct([for circuit in values(var.interconnects) : circuit.location])) == length(var.interconnects)
    error_message = "Hybrid connectivity requires at least two circuits in distinct facilities."
  }
}
variable "cloud_router" {
  type = object({
    name           = string
    asn            = number
    advertise_mode = string
    advertised_ip_ranges = set(object({
      range       = string
      description = string
    }))
  })
}
variable "bgp_md5_authentication" { type = bool }
variable "macsec_enabled" { type = bool }
variable "labels" {
  type    = map(string)
  default = {}
}
