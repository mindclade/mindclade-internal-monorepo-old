# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "parent" {
  description = "Organization or folder on which to create aggregated sinks, as organizations/<id> or folders/<id>."
  type        = string

  validation {
    condition     = can(regex("^(organizations|folders)/[0-9]{1,25}$", var.parent))
    error_message = "parent must be organizations/<numeric id> or folders/<numeric id>."
  }
}

variable "project_id" {
  description = "Project that owns destination buckets and their billing/perimeter boundary."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid Google Cloud project ID."
  }
}

variable "include_children" {
  description = "Include descendant folders and projects in the aggregated sink."
  type        = bool
  default     = true
}

variable "logging_bucket_location" {
  description = "Location for Cloud Logging destinations; fixed when each bucket is created."
  type        = string
  default     = "global"

  validation {
    condition     = length(trimspace(var.logging_bucket_location)) > 0
    error_message = "logging_bucket_location must not be empty."
  }
}

variable "sinks" {
  description = <<-EOT
    Aggregated sinks keyed by a stable sink name. "logging" creates a queryable Cloud
    Logging bucket. "storage" creates a deletion-protected GCS archive bucket. A storage
    retention lock is irreversible and additionally requires retention_lock_confirmation.
  EOT
  type = map(object({
    description = string
    destination = string
    filter      = string

    enable_analytics = optional(bool, false)
    retention_days   = optional(number, 30)

    bucket = optional(object({
      name                       = string
      location                   = string
      access_log_bucket_name     = string
      access_log_object_prefix   = optional(string, "log-archive/")
      encryption_key             = optional(string)
      retention_days             = optional(number)
      lock_retention_policy      = optional(bool, false)
      soft_delete_retention_days = optional(number, 30)
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
    condition     = alltrue([for sink in values(var.sinks) : contains(["logging", "storage"], sink.destination)])
    error_message = "destination must be either logging or storage."
  }

  validation {
    condition     = alltrue([for sink in values(var.sinks) : sink.destination != "storage" || sink.bucket != null])
    error_message = "Every storage sink requires a bucket block."
  }

  validation {
    condition = alltrue([
      for name, sink in var.sinks :
      can(regex("^[a-z][a-z0-9-]{1,98}[a-z0-9]$", name)) &&
      length(sink.description) <= 8000 &&
      length(trimspace(sink.filter)) > 0 &&
      length(sink.filter) <= 20000
    ])
    error_message = "Sink keys must be 3-100 lowercase characters; descriptions and non-empty filters must remain within Logging API bounds."
  }

  validation {
    condition     = alltrue([for sink in values(var.sinks) : !sink.enable_analytics || sink.destination == "logging"])
    error_message = "enable_analytics applies only to a logging destination."
  }

  validation {
    condition = alltrue([
      for sink in values(var.sinks) :
      sink.destination != "logging" || (sink.retention_days >= 1 && sink.retention_days <= 3650 && floor(sink.retention_days) == sink.retention_days)
    ])
    error_message = "Cloud Logging bucket retention must be a whole number from 1 through 3650 days."
  }

  validation {
    condition = alltrue([
      for sink in values(var.sinks) : sink.destination != "storage" ? true : (
        can(regex("^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$", sink.bucket.name)) &&
        can(regex("^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$", sink.bucket.access_log_bucket_name)) &&
        sink.bucket.access_log_bucket_name != sink.bucket.name &&
        length(sink.bucket.access_log_object_prefix) >= 1 &&
        length(sink.bucket.access_log_object_prefix) <= 900 &&
        !strcontains(sink.bucket.access_log_object_prefix, "\n") &&
        !strcontains(sink.bucket.access_log_object_prefix, "\r") &&
        length(trimspace(sink.bucket.location)) > 0 &&
        (sink.bucket.encryption_key == null || can(regex("^projects/[^/]+/locations/[^/]+/keyRings/[^/]+/cryptoKeys/[^/]+$", sink.bucket.encryption_key))) &&
        (sink.bucket.retention_days == null ? true : (sink.bucket.retention_days >= 1 && sink.bucket.retention_days <= 36500 && floor(sink.bucket.retention_days) == sink.bucket.retention_days)) &&
        sink.bucket.soft_delete_retention_days >= 7 &&
        sink.bucket.soft_delete_retention_days <= 90 &&
        floor(sink.bucket.soft_delete_retention_days) == sink.bucket.soft_delete_retention_days
      )
    ])
    error_message = "Storage bucket and distinct access-log bucket names, access-log prefix, location, CMEK name, retention, and 7-90 day soft-delete window must be valid and bounded."
  }

  validation {
    condition = alltrue(flatten([
      for sink in values(var.sinks) : [
        for rule in sink.bucket == null ? [] : sink.bucket.lifecycle_rules :
        rule.age >= 1 && floor(rule.age) == rule.age &&
        contains(["Delete", "SetStorageClass"], rule.action) &&
        (rule.action != "SetStorageClass" || contains(["STANDARD", "NEARLINE", "COLDLINE", "ARCHIVE"], rule.storage_class)) &&
        (rule.action == "SetStorageClass" || rule.storage_class == null)
      ]
    ]))
    error_message = "Storage lifecycle rules require a positive whole-number age and a valid Delete or SetStorageClass action."
  }

  validation {
    condition = alltrue(flatten([
      for sink in values(var.sinks) : [
        for exclusion in sink.exclusions :
        can(regex("^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$", exclusion.name)) &&
        length(exclusion.description) <= 8000 &&
        length(trimspace(exclusion.filter)) > 0 &&
        length(exclusion.filter) <= 20000
      ]
    ]))
    error_message = "Exclusion names, descriptions, and non-empty filters must remain within Logging API bounds."
  }
}

variable "retention_lock_confirmation" {
  description = "Exact irreversible-action acknowledgement required when any GCS archive retention policy is locked."
  type        = string
  default     = null
  sensitive   = true
}

variable "default_sink_retention_days" {
  description = "Retention for only project_id's own _Default log bucket; 0 leaves that bucket unmanaged. Descendant project buckets are outside this module's scope."
  type        = number
  default     = 30

  validation {
    condition     = var.default_sink_retention_days >= 0 && var.default_sink_retention_days <= 3650 && floor(var.default_sink_retention_days) == var.default_sink_retention_days
    error_message = "default_sink_retention_days must be a whole number from 0 through 3650."
  }
}

variable "labels" {
  description = "Labels applied to GCS archive buckets. Cloud Logging buckets do not carry labels."
  type        = map(string)
  default     = {}

  validation {
    condition = length(var.labels) <= 64 && alltrue([
      for key, value in var.labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
      can(regex("^$|^[a-z0-9][a-z0-9_-]{0,62}$", value))
    ])
    error_message = "labels must contain at most 64 valid lowercase Google Cloud label pairs."
  }
}
