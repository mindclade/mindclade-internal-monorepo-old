# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "parent" {
  description = "Organization or folder the sinks are created on, as organizations/<id> or folders/<id>"
  type        = string

  validation {
    condition     = can(regex("^(organizations|folders)/[0-9]{1,25}$", var.parent))
    error_message = "parent must be organizations/<numeric id> or folders/<numeric id>."
  }
}

variable "project_id" {
  description = "Project that holds the destination buckets. Every sink writes into this one project so that log storage has a single billing owner and a single perimeter."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid Google Cloud project ID."
  }
}

variable "include_children" {
  description = <<-EOT
    Whether the sink captures logs from every descendant of the parent. false on an
    organization sink means it captures only logs written against the organization resource
    itself — which is almost nothing, and looks identical to a working sink.
  EOT
  type        = bool
  default     = true
}

variable "sinks" {
  description = <<-EOT
    Sinks keyed by a stable short name, which also names the destination bucket.

    `destination` selects the backend:
      logging  a Cloud Logging bucket in project_id — queryable, optionally via Log Analytics
      storage  a GCS bucket in project_id — cheap, for the tier nobody queries interactively

    `bucket` is required for `storage` and ignored for `logging`, where retention_days and
    enable_analytics configure the log bucket instead.
  EOT
  type = map(object({
    description = string
    destination = string
    filter      = string

    # logging destinations
    enable_analytics = optional(bool, false)
    retention_days   = optional(number, 30)

    # storage destinations
    bucket = optional(object({
      name           = string
      location       = string
      encryption_key = optional(string)
      retention_days = optional(number)
      lifecycle_rules = optional(list(object({
        age           = number
        action        = string
        storage_class = optional(string)
      })), [])
    }))

    exclusions = optional(list(object({
      name        = string
      description = optional(string, "")
      filter      = string
    })), [])
  }))

  validation {
    condition     = alltrue([for k, v in var.sinks : contains(["logging", "storage"], v.destination)])
    error_message = "destination must be either logging or storage."
  }

  validation {
    condition     = alltrue([for k, v in var.sinks : v.destination != "storage" || v.bucket != null])
    error_message = "A storage sink needs a bucket block; without one there is nowhere to write."
  }

  validation {
    condition     = alltrue([for k, v in var.sinks : length(trimspace(v.filter)) > 0])
    error_message = "An empty filter matches everything, which is never what a named sink means. Use a filter that says so explicitly."
  }

  validation {
    condition     = alltrue([for k, v in var.sinks : can(regex("^[a-z][a-z0-9-]{1,98}[a-z0-9]$", k))])
    error_message = "Each sink key must be a 3-100 character lowercase name."
  }

  # Log Analytics can only be turned on when the bucket is created. Enabling it later
  # requires deleting and recreating the bucket, taking every log entry in it.
  validation {
    condition     = alltrue([for k, v in var.sinks : !v.enable_analytics || v.destination == "logging"])
    error_message = "enable_analytics applies only to a logging destination."
  }

  validation {
    condition = alltrue([
      for k, v in var.sinks :
      v.destination != "logging" || (v.retention_days >= 1 && v.retention_days <= 3650)
    ])
    error_message = "Cloud Logging bucket retention must be between 1 and 3650 days."
  }

  validation {
    condition = alltrue(flatten([
      for k, v in var.sinks : [
        for e in v.exclusions : length(trimspace(e.filter)) > 0
      ]
    ]))
    error_message = "An exclusion with an empty filter excludes nothing and reads as if it does."
  }
}

variable "default_sink_retention_days" {
  description = <<-EOT
    Retention for each project's own _Default log bucket. Set to 0 to leave it untouched.

    Not disabled outright: local retention is what makes `gcloud logging read` in a project
    work during an incident, before anyone has opened BigQuery.
  EOT
  type        = number
  default     = 30

  validation {
    condition     = var.default_sink_retention_days >= 0 && var.default_sink_retention_days <= 3650
    error_message = "default_sink_retention_days must be between 0 and 3650."
  }
}

variable "labels" {
  description = "Labels applied to every GCS bucket this module creates. Cloud Logging buckets do not carry labels."
  type        = map(string)
  default     = {}
}
