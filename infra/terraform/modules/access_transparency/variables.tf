# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "org_id" {
  description = "Numeric organization id whose Access Transparency logs are exported"
  type        = string

  validation {
    condition     = can(regex("^[0-9]{1,25}$", var.org_id))
    error_message = "org_id must be the bare numeric id, without the organizations/ prefix."
  }
}

variable "project_id" {
  description = "Project holding the archive bucket, the notification channels, and the alert policy"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid Google Cloud project ID."
  }
}

variable "sink" {
  description = <<-EOT
    Where Access Transparency entries are kept.

    These are low-volume — tens of entries a year on a healthy estate — so retention costs
    almost nothing, and the alternative is discovering the window was too short during the
    investigation that needed it.
  EOT
  type = object({
    name        = string
    destination = optional(string, "storage")
    filter      = string
    bucket = object({
      name           = string
      location       = string
      encryption_key = optional(string)
      retention_days = optional(number, 2555)
    })
  })

  validation {
    condition     = var.sink.destination == "storage"
    error_message = "Only a storage destination is supported; an access record belongs somewhere append-only."
  }

  validation {
    condition     = length(trimspace(var.sink.filter)) > 0
    error_message = "An empty filter would export every log in the organization into the access-transparency bucket."
  }

  # The filter has to actually select Access Transparency entries. A filter that selects
  # nothing produces an empty bucket that is indistinguishable from an estate nobody
  # accessed — which is exactly the wrong conclusion to draw silently.
  validation {
    condition     = can(regex("access_transparency", var.sink.filter))
    error_message = "The sink filter must reference access_transparency, or it exports the wrong logs."
  }

  validation {
    condition     = var.sink.bucket.retention_days == null || var.sink.bucket.retention_days >= 365
    error_message = "Access records are kept for at least a year; a shorter window defeats the point of the export."
  }
}

variable "alert" {
  description = <<-EOT
    Alert raised on every access.

    Not a threshold and not a daily digest. The volume justifies it — this fires a handful of
    times a year — and the value of the record is entirely in someone reading it while the
    support case that prompted it is still open.

    `notification_channels` takes email addresses; a channel is created for each.
  EOT
  type = object({
    display_name          = string
    severity              = optional(string, "WARNING")
    filter                = string
    notification_channels = optional(list(string), [])
    documentation         = optional(string, "")
  })
  default = null

  validation {
    condition     = var.alert == null || contains(["CRITICAL", "ERROR", "WARNING"], var.alert.severity)
    error_message = "severity must be CRITICAL, ERROR, or WARNING."
  }

  validation {
    condition = var.alert == null || alltrue([
      for c in var.alert.notification_channels :
      can(regex("^[^@[:space:]]+@[^@[:space:]]+\\.[a-z]{2,}$", c))
    ])
    error_message = "notification_channels must be email addresses; a channel is created for each."
  }

  # An alert with no destination fires into nothing. Google accepts it, the policy shows as
  # enabled, and the first anyone knows is that a page never arrived.
  validation {
    condition     = var.alert == null || length(var.alert.notification_channels) > 0
    error_message = "An alert with no notification channels is enabled and silent."
  }
}

variable "labels" {
  description = "Labels applied to the archive bucket."
  type        = map(string)
  default     = {}
}
