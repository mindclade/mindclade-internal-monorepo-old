# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Project that owns the Bazel remote-cache bucket"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid 6-30 character GCP project ID."
  }
}

variable "bucket_name" {
  description = "Globally unique bucket name"
  type        = string

  validation {
    condition     = length(var.bucket_name) >= 3 && length(var.bucket_name) <= 63 && can(regex("^[a-z0-9][a-z0-9._-]*[a-z0-9]$", var.bucket_name))
    error_message = "bucket_name must be a valid 3-63 character Cloud Storage bucket name."
  }
}

variable "location" {
  description = "Cloud Storage region, dual-region, or multi-region"
  type        = string

  validation {
    condition     = length(trimspace(var.location)) > 0
    error_message = "location must not be empty."
  }
}

variable "kms_key_name" {
  description = "CMEK CryptoKey resource name in the bucket location"
  type        = string

  validation {
    condition = (
      can(regex("^projects/[^/]+/locations/[^/]+/keyRings/[^/]+/cryptoKeys/[^/]+$", var.kms_key_name)) &&
      try(lower(split("/", var.kms_key_name)[3]), "") == lower(var.location)
    )
    error_message = "kms_key_name must be a complete CryptoKey resource name in location."
  }
}

variable "access_log_bucket" {
  description = "Existing separately governed bucket that receives server-access logs"
  type        = string

  validation {
    condition     = length(var.access_log_bucket) >= 3 && length(var.access_log_bucket) <= 63 && can(regex("^[a-z0-9][a-z0-9._-]*[a-z0-9]$", var.access_log_bucket))
    error_message = "access_log_bucket must be a valid 3-63 character bucket name."
  }

  validation {
    condition     = var.access_log_bucket != var.bucket_name
    error_message = "access_log_bucket must be separate from the cache bucket."
  }
}

variable "access_log_object_prefix" {
  description = "Non-sensitive prefix for this cache's access-log objects"
  type        = string
  default     = "bazel-remote-cache/"

  validation {
    condition     = length(var.access_log_object_prefix) >= 1 && length(var.access_log_object_prefix) <= 900
    error_message = "access_log_object_prefix must contain 1-900 characters."
  }
}

variable "environment" {
  description = "Environment governance label"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.environment))
    error_message = "environment must be a valid non-empty GCP label value."
  }
}

variable "owner" {
  description = "Accountable team governance label"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.owner))
    error_message = "owner must be a valid non-empty GCP label value."
  }
}

variable "data_classification" {
  description = "Governance classification for cached build outputs; public classification is forbidden"
  type        = string
  default     = "internal"

  validation {
    condition     = contains(["internal", "confidential", "restricted"], var.data_classification)
    error_message = "data_classification must be internal, confidential, or restricted."
  }
}

variable "labels" {
  description = "Additional labels; cache and storage governance labels take precedence"
  type        = map(string)
  default     = {}

  validation {
    condition = length(var.labels) <= 58 && alltrue([
      for key, value in var.labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
      can(regex("^$|^[a-z0-9][a-z0-9_-]{0,62}$", value))
    ])
    error_message = "labels must contain at most 58 valid lowercase pairs."
  }
}

variable "reader_members" {
  description = "Workload identities and groups granted additive objectViewer access"
  type        = set(string)
  default     = []

  validation {
    condition = length(var.reader_members) <= 64 && alltrue([
      for member in var.reader_members :
      !strcontains(member, "*") && can(regex("^(serviceAccount|group):[^[:space:]]+$|^principal(Set)?://iam\\.googleapis\\.com/[^[:space:]]+$", member))
    ])
    error_message = "reader_members must contain at most 64 non-wildcard workload identities or groups; public, domain, and direct-user principals are forbidden."
  }
}

variable "writer_members" {
  description = "Non-public cache identities granted additive create-only plus read access"
  type        = set(string)

  validation {
    condition = length(var.writer_members) >= 1 && length(var.writer_members) <= 32 && alltrue([
      for member in var.writer_members :
      !strcontains(member, "*") && can(regex("^serviceAccount:[^[:space:]]+$|^principal(Set)?://iam\\.googleapis\\.com/[^[:space:]]+$", member))
    ])
    error_message = "writer_members must contain 1-32 non-wildcard workload identities; public, group, domain, and direct-user principals are forbidden."
  }
}

variable "cache_ttl_days" {
  description = "Age at which rebuildable live cache entries are deleted"
  type        = number
  default     = 14

  validation {
    condition     = var.cache_ttl_days >= 1 && var.cache_ttl_days <= 90 && floor(var.cache_ttl_days) == var.cache_ttl_days
    error_message = "cache_ttl_days must be a whole number from 1 through 90."
  }
}

variable "noncurrent_version_ttl_days" {
  description = "Age at which noncurrent cache generations are deleted while retaining one newer generation"
  type        = number
  default     = 1

  validation {
    condition     = var.noncurrent_version_ttl_days >= 1 && var.noncurrent_version_ttl_days <= 30 && floor(var.noncurrent_version_ttl_days) == var.noncurrent_version_ttl_days
    error_message = "noncurrent_version_ttl_days must be a whole number from 1 through 30."
  }
}

variable "soft_delete_retention_days" {
  description = "Recovery window after cache-object deletion"
  type        = number
  default     = 7

  validation {
    condition     = var.soft_delete_retention_days >= 7 && var.soft_delete_retention_days <= 30 && floor(var.soft_delete_retention_days) == var.soft_delete_retention_days
    error_message = "soft_delete_retention_days must be a whole number from 7 through 30 for this rebuildable cache."
  }
}

variable "retention_period_seconds" {
  description = "Minimum object-retention period; bounded below the cache TTL and deliberately not locked"
  type        = number
  default     = 86400

  validation {
    condition = (
      var.retention_period_seconds >= 1 &&
      floor(var.retention_period_seconds) == var.retention_period_seconds &&
      var.retention_period_seconds <= var.cache_ttl_days * 86400
    )
    error_message = "retention_period_seconds must be a positive whole number no greater than cache_ttl_days."
  }
}
