# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Project that owns the bucket"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid GCP project ID."
  }
}

variable "name" {
  description = "Globally unique bucket name"
  type        = string

  validation {
    condition     = length(var.name) >= 3 && length(var.name) <= 63 && can(regex("^[a-z0-9][a-z0-9._-]*[a-z0-9]$", var.name))
    error_message = "name must be a valid 3-63 character Cloud Storage bucket name."
  }
}

variable "location" {
  description = "Region, dual-region, or multi-region for the bucket"
  type        = string

  validation {
    condition     = length(trimspace(var.location)) > 0
    error_message = "location must not be empty."
  }
}

variable "storage_class" {
  description = "Default storage class"
  type        = string
  default     = "STANDARD"

  validation {
    condition     = contains(["STANDARD", "MULTI_REGIONAL", "REGIONAL", "NEARLINE", "COLDLINE", "ARCHIVE"], var.storage_class)
    error_message = "storage_class must be a supported Cloud Storage class."
  }
}

variable "environment" {
  description = "Environment governance label"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.environment))
    error_message = "environment must be a valid GCP label value."
  }
}

variable "owner" {
  description = "Accountable team governance label"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.owner))
    error_message = "owner must be a valid GCP label value."
  }
}

variable "data_classification" {
  description = "Data-classification governance label"
  type        = string
  default     = "confidential"

  validation {
    condition     = contains(["public", "internal", "confidential", "restricted"], var.data_classification)
    error_message = "data_classification must be public, internal, confidential, or restricted."
  }
}

variable "labels" {
  description = "Additional labels; baseline governance labels take precedence"
  type        = map(string)
  default     = {}

  validation {
    condition = length(var.labels) <= 60 && alltrue([
      for key, value in var.labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
      can(regex("^$|^[a-z0-9][a-z0-9_-]{0,62}$", value))
    ])
    error_message = "labels must contain at most 60 valid lowercase pairs, leaving room for module governance labels."
  }
}

variable "kms_key_name" {
  description = "Optional CryptoKey resource name; grant the Storage service agent encrypt/decrypt separately"
  type        = string
  default     = null

  validation {
    condition = var.kms_key_name == null || (
      can(regex("^projects/[^/]+/locations/[^/]+/keyRings/[^/]+/cryptoKeys/[^/]+$", coalesce(var.kms_key_name, ""))) &&
      try(lower(split("/", var.kms_key_name)[3]), "") == lower(var.location)
    )
    error_message = "kms_key_name must be a full CryptoKey resource name in the bucket location or null."
  }
}

variable "access_log_bucket" {
  description = "Existing separately governed bucket receiving Cloud Storage server-access logs"
  type        = string

  validation {
    condition     = length(var.access_log_bucket) >= 3 && length(var.access_log_bucket) <= 63 && can(regex("^[a-z0-9][a-z0-9._-]*[a-z0-9]$", var.access_log_bucket))
    error_message = "access_log_bucket must be a valid 3-63 character bucket name."
  }
}

variable "access_log_object_prefix" {
  description = "Non-sensitive prefix for access-log objects"
  type        = string
  default     = "storage-access/"

  validation {
    condition     = length(var.access_log_object_prefix) > 0 && length(var.access_log_object_prefix) <= 900
    error_message = "access_log_object_prefix must contain 1-900 characters."
  }
}

variable "versioning_enabled" {
  description = "Retain noncurrent object generations"
  type        = bool
  default     = true
}

variable "create_only_workload" {
  description = "Enforce the additive IAM/lifecycle boundary used by manifest-last NOVA training checkpoint publication; clients must still send ifGenerationMatch=0"
  type        = bool
  default     = false
}

variable "soft_delete_retention_days" {
  description = "Soft-delete recovery window; Cloud Storage supports 7-90 days"
  type        = number
  default     = 30

  validation {
    condition     = var.soft_delete_retention_days >= 7 && var.soft_delete_retention_days <= 90 && floor(var.soft_delete_retention_days) == var.soft_delete_retention_days
    error_message = "soft_delete_retention_days must be a whole number from 7 through 90."
  }
}

variable "retention_period_seconds" {
  description = "Optional minimum object retention period; null disables Bucket Lock policy"
  type        = number
  default     = null

  validation {
    condition     = var.retention_period_seconds == null || (var.retention_period_seconds >= 1 && var.retention_period_seconds <= 3155760000 && floor(var.retention_period_seconds) == var.retention_period_seconds)
    error_message = "retention_period_seconds must be null or a whole number from 1 through 3155760000."
  }
}

variable "lock_retention_policy" {
  description = "Permanently lock the retention policy; irreversible"
  type        = bool
  default     = false
}

variable "retention_lock_confirmation" {
  description = "Exact irreversible-action acknowledgement required when lock_retention_policy is true"
  type        = string
  default     = null
  sensitive   = true
}

variable "lifecycle_rules" {
  description = "Cost and retention lifecycle rules; keep deletion decisions explicit"
  type = list(object({
    action                     = string
    storage_class              = optional(string)
    age_days                   = optional(number)
    days_since_noncurrent_time = optional(number)
    matches_prefix             = optional(list(string))
    matches_suffix             = optional(list(string))
    num_newer_versions         = optional(number)
    with_state                 = optional(string)
  }))
  default = [{
    action        = "AbortIncompleteMultipartUpload"
    age_days      = 7
    with_state    = null
    storage_class = null
  }]

  validation {
    condition = alltrue([
      for rule in var.lifecycle_rules :
      contains(["Delete", "SetStorageClass", "AbortIncompleteMultipartUpload"], rule.action) &&
      (rule.age_days == null || (rule.age_days >= 1 && floor(rule.age_days) == rule.age_days)) &&
      (rule.days_since_noncurrent_time == null || (rule.days_since_noncurrent_time >= 1 && floor(rule.days_since_noncurrent_time) == rule.days_since_noncurrent_time)) &&
      (rule.num_newer_versions == null || (rule.num_newer_versions >= 1 && floor(rule.num_newer_versions) == rule.num_newer_versions)) &&
      (rule.with_state == null || contains(["ANY", "LIVE", "ARCHIVED"], rule.with_state)) &&
      (
        rule.action == "SetStorageClass" ? contains(["STANDARD", "NEARLINE", "COLDLINE", "ARCHIVE"], rule.storage_class) : rule.storage_class == null
      ) &&
      (
        rule.action == "AbortIncompleteMultipartUpload" ? (
          rule.age_days != null &&
          rule.days_since_noncurrent_time == null &&
          rule.num_newer_versions == null &&
          rule.with_state == null
          ) : (
          rule.age_days != null ||
          rule.days_since_noncurrent_time != null ||
          rule.num_newer_versions != null
        )
      )
    ])
    error_message = "Lifecycle rules require positive whole-number conditions; SetStorageClass needs a supported target class, while AbortIncompleteMultipartUpload permits only age, prefix, and suffix conditions."
  }
}

variable "object_viewers" {
  description = "IAM members allowed to read objects"
  type        = set(string)
  default     = []

  validation {
    condition     = length(setintersection(var.object_viewers, toset(["allUsers", "allAuthenticatedUsers"]))) == 0
    error_message = "Public and all-authenticated principals are forbidden in object_viewers."
  }
}

variable "object_creators" {
  description = "IAM members allowed to create, but not overwrite or delete, objects"
  type        = set(string)
  default     = []

  validation {
    condition     = length(setintersection(var.object_creators, toset(["allUsers", "allAuthenticatedUsers"]))) == 0
    error_message = "Public and all-authenticated principals are forbidden in object_creators."
  }
}

variable "object_admins" {
  description = "IAM members allowed to manage objects; use sparingly"
  type        = set(string)
  default     = []

  validation {
    condition     = length(setintersection(var.object_admins, toset(["allUsers", "allAuthenticatedUsers"]))) == 0
    error_message = "Public and all-authenticated principals are forbidden in object_admins."
  }
}
