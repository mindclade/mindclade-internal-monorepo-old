# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "org_id" {
  description = "Numeric organization id Security Command Center is configured on"
  type        = string

  validation {
    condition     = can(regex("^[0-9]{1,25}$", var.org_id))
    error_message = "org_id must be the bare numeric id, without the organizations/ prefix."
  }
}

variable "project_id" {
  description = "Project holding the notification topics and the BigQuery export"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid Google Cloud project ID."
  }
}

variable "location" {
  description = "Location for the BigQuery findings dataset"
  type        = string
  default     = "US"
}

variable "services" {
  description = <<-EOT
    Built-in SCC services keyed by service name, each ENABLE or DISABLE.

    Set explicitly rather than left at the tier default: the default changes with the tier,
    and a silent downgrade is indistinguishable from "no findings".
  EOT
  type        = map(string)
  default     = {}

  validation {
    condition     = alltrue([for k, v in var.services : contains(["ENABLE", "DISABLE"], v)])
    error_message = "Each service must be ENABLE or DISABLE."
  }
}

variable "notifications" {
  description = <<-EOT
    Notification configs keyed by short name. Each creates its own Pub/Sub topic.

    Separate destinations are the point: a stream nobody watches and a page nobody can ignore
    are different things, and conflating them produces a channel people mute.
  EOT
  type = map(object({
    description = string
    filter      = string
    pubsub_topic = object({
      name = string
    })
  }))
  default = {}

  validation {
    condition     = alltrue([for k, v in var.notifications : length(trimspace(v.filter)) > 0])
    error_message = "An empty filter notifies on everything, which is never what a named config means."
  }

  validation {
    condition     = alltrue([for k, v in var.notifications : can(regex("^[a-z][a-z0-9-]{2,126}[a-z0-9]$", k))])
    error_message = "Each notification key must be a 4-128 character lowercase name."
  }
}

variable "bigquery_export" {
  description = <<-EOT
    Continuous export of findings into BigQuery, so a finding can be joined against the audit
    dataset. The join that matters: a finding says a service account has excessive
    permissions, and the audit log says whether it used them.
  EOT
  type = object({
    dataset_id = string
    location   = optional(string)
    filter     = string
  })
  default = null

  validation {
    condition     = var.bigquery_export == null || can(regex("^[a-zA-Z0-9_]{1,1024}$", var.bigquery_export.dataset_id))
    error_message = "dataset_id may contain only letters, numbers, and underscores."
  }
}

variable "mute_configs" {
  description = <<-EOT
    Standing mutes, keyed by short name.

    Muting in the UI leaves no reason, no owner, and no expiry. Declared here, a mute is a
    pull request somebody reviews — and one that shows up in a diff when it is still present
    a year later.
  EOT
  type = map(object({
    description = string
    filter      = string
  }))
  default = {}

  # A mute with no reason is a finding somebody found inconvenient.
  validation {
    condition     = alltrue([for k, v in var.mute_configs : length(trimspace(v.description)) >= 30])
    error_message = "Every mute needs a description of at least 30 characters saying what is reported and why it is expected."
  }

  validation {
    condition     = alltrue([for k, v in var.mute_configs : length(trimspace(v.filter)) > 0])
    error_message = "A mute with an empty filter mutes every finding in the organization."
  }
}

variable "labels" {
  description = "Labels applied to the Pub/Sub topics and BigQuery dataset."
  type        = map(string)
  default     = {}
}
