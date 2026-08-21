# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

variable "project_id" { type = string }
variable "parallelstore" {
  description = "Parallelstore scratch instances keyed by stable identity."
  type = map(object({
    name              = string
    location          = string
    capacity_gib      = number
    deployment_type   = string
    network           = string
    reserved_ip_range = string
    gcs_import = optional(object({
      source = string
    }))
  }))
  validation {
    condition = length(var.parallelstore) > 0 && alltrue([
      for instance in values(var.parallelstore) :
      instance.capacity_gib >= 12000 && contains(["SCRATCH", "PERSISTENT"], instance.deployment_type)
    ])
    error_message = "Parallelstore instances require supported capacity and deployment type."
  }
}
variable "labels" {
  type    = map(string)
  default = {}
}
