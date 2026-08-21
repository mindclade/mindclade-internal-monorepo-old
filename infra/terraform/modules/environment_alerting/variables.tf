# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

variable "project_id" { type = string }
variable "metrics_scope_project" { type = string }
variable "cluster_name" { type = string }
variable "notification_channels" {
  type = map(object({
    type  = string
    email = optional(string)
  }))
  validation {
    condition = alltrue([
      for channel in values(var.notification_channels) :
      channel.type == "email" && channel.email != null && can(regex("^[^@[:space:]]+@[^@[:space:]]+$", channel.email))
    ])
    error_message = "This source-qualified composition accepts explicit email channels only."
  }
}
variable "default_notification_channels" {
  type = set(string)
  validation {
    condition     = length(setsubtract(var.default_notification_channels, keys(var.notification_channels))) == 0
    error_message = "Default notification channel keys must exist in notification_channels."
  }
}
variable "alert_policies" {
  type = map(object({
    display_name = string
    severity     = string
    condition = object({
      filter          = string
      comparison      = string
      threshold_value = number
      duration        = string
      aligner         = string
    })
    documentation = string
  }))
  validation {
    condition = length(var.alert_policies) > 0 && alltrue([
      for policy in values(var.alert_policies) :
      contains(["CRITICAL", "ERROR", "WARNING"], policy.severity) &&
      can(regex("^COMPARISON_", policy.condition.comparison)) &&
      can(regex("^ALIGN_", policy.condition.aligner))
    ])
    error_message = "Alert policies require explicit severity, comparison, and aligner contracts."
  }
}
variable "labels" {
  type    = map(string)
  default = {}
}
