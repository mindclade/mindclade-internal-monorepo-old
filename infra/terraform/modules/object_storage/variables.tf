# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Project that owns all composed buckets."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid Google Cloud project ID."
  }
}

variable "environment" {
  description = "Environment governance label."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.environment))
    error_message = "environment must be a valid Google Cloud label value."
  }
}

variable "owner" {
  description = "Accountable team governance label."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.owner))
    error_message = "owner must be a valid Google Cloud label value."
  }
}

variable "storage_service_agent_email" {
  description = "Google-managed Cloud Storage service-agent email for project_id, used to report required CMEK grants."
  type        = string

  validation {
    condition     = can(regex("^service-[0-9]+@gs-project-accounts[.]iam[.]gserviceaccount[.]com$", var.storage_service_agent_email))
    error_message = "storage_service_agent_email must be service-<project-number>@gs-project-accounts.iam.gserviceaccount.com."
  }
}

variable "labels" {
  description = "Additional labels shared by every bucket; class and storage baseline labels take precedence."
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

variable "upstream_access_log_bucket_name" {
  description = "Separately governed existing bucket that receives access logs for this composition's access-log bucket."
  type        = string

  validation {
    condition     = length(var.upstream_access_log_bucket_name) >= 3 && length(var.upstream_access_log_bucket_name) <= 63 && can(regex("^[a-z0-9][a-z0-9._-]*[a-z0-9]$", var.upstream_access_log_bucket_name))
    error_message = "upstream_access_log_bucket_name must be a valid 3-63 character bucket name."
  }
}

variable "access_log_bucket" {
  description = "Managed bucket receiving server-access logs from all data and AI artifact buckets."
  type = object({
    name                       = string
    location                   = string
    kms_key_name               = string
    storage_class              = optional(string, "STANDARD")
    retention_days             = optional(number, 365)
    soft_delete_retention_days = optional(number, 30)
    viewers                    = optional(set(string), [])
    labels                     = optional(map(string), {})
  })

  validation {
    condition = (
      length(var.access_log_bucket.name) >= 3 &&
      length(var.access_log_bucket.name) <= 63 &&
      can(regex("^[a-z0-9][a-z0-9._-]*[a-z0-9]$", var.access_log_bucket.name)) &&
      length(trimspace(var.access_log_bucket.location)) > 0 &&
      can(regex("^projects/[^/]+/locations/[^/]+/keyRings/[^/]+/cryptoKeys/[^/]+$", var.access_log_bucket.kms_key_name)) &&
      contains(["STANDARD", "NEARLINE"], var.access_log_bucket.storage_class) &&
      var.access_log_bucket.retention_days >= 365 &&
      var.access_log_bucket.retention_days <= 3650 &&
      floor(var.access_log_bucket.retention_days) == var.access_log_bucket.retention_days &&
      var.access_log_bucket.soft_delete_retention_days >= 7 &&
      var.access_log_bucket.soft_delete_retention_days <= 90 &&
      floor(var.access_log_bucket.soft_delete_retention_days) == var.access_log_bucket.soft_delete_retention_days
    )
    error_message = "The access-log bucket requires a valid name/location/CMEK, STANDARD or NEARLINE class, 365-3650 day retention, and 7-90 day soft delete."
  }

  validation {
    condition = alltrue([
      for member in var.access_log_bucket.viewers :
      !contains(["allUsers", "allAuthenticatedUsers"], member) && length(trimspace(member)) > 0
    ])
    error_message = "Access-log viewers must be non-empty, non-public IAM members."
  }
}

variable "data_buckets" {
  description = "Governed mutable data buckets keyed by stable Terraform identity."
  type = map(object({
    name                       = string
    location                   = string
    kms_key_name               = string
    data_class                 = string
    storage_class              = optional(string, "STANDARD")
    data_classification        = optional(string, "restricted")
    soft_delete_retention_days = optional(number, 30)
    retention_period_seconds   = optional(number)
    readers                    = optional(set(string), [])
    writers                    = optional(set(string), [])
    admins                     = optional(set(string), [])
    labels                     = optional(map(string), {})
    lifecycle_rules = optional(list(object({
      action                     = string
      storage_class              = optional(string)
      age_days                   = optional(number)
      days_since_noncurrent_time = optional(number)
      matches_prefix             = optional(list(string))
      matches_suffix             = optional(list(string))
      num_newer_versions         = optional(number)
      with_state                 = optional(string)
      })), [{
      action        = "AbortIncompleteMultipartUpload"
      age_days      = 7
      storage_class = null
      with_state    = null
    }])
  }))
  default = {}

  validation {
    condition = alltrue([
      for bucket in values(var.data_buckets) :
      length(bucket.name) >= 3 &&
      length(bucket.name) <= 63 &&
      can(regex("^[a-z0-9][a-z0-9._-]*[a-z0-9]$", bucket.name)) &&
      length(trimspace(bucket.location)) > 0 &&
      can(regex("^projects/[^/]+/locations/[^/]+/keyRings/[^/]+/cryptoKeys/[^/]+$", bucket.kms_key_name)) &&
      contains(["raw", "curated", "reference", "dataset", "evidence"], bucket.data_class) &&
      contains(["STANDARD", "NEARLINE", "COLDLINE", "ARCHIVE"], bucket.storage_class) &&
      contains(["internal", "confidential", "restricted"], bucket.data_classification) &&
      bucket.soft_delete_retention_days >= 7 &&
      bucket.soft_delete_retention_days <= 90 &&
      floor(bucket.soft_delete_retention_days) == bucket.soft_delete_retention_days &&
      (
        bucket.retention_period_seconds == null ? true :
        (
          bucket.retention_period_seconds >= 1 &&
          bucket.retention_period_seconds <= 3155760000 &&
          floor(bucket.retention_period_seconds) == bucket.retention_period_seconds
        )
      )
    ])
    error_message = "Data buckets require bounded names, locations, CMEK, class, classification, retention, and soft-delete settings."
  }

  validation {
    condition = alltrue(flatten([
      for bucket in values(var.data_buckets) : [
        for member in setunion(bucket.readers, bucket.writers, bucket.admins) :
        !contains(["allUsers", "allAuthenticatedUsers"], member) && length(trimspace(member)) > 0
      ]
    ]))
    error_message = "Data-bucket IAM members must be non-empty and non-public."
  }

  validation {
    condition = (
      length(var.data_buckets) + length(var.ai_artifact_buckets) > 0 &&
      length(distinct(concat(
        [var.access_log_bucket.name, var.upstream_access_log_bucket_name],
        [for bucket in values(var.data_buckets) : bucket.name],
        [for bucket in values(var.ai_artifact_buckets) : bucket.name],
      ))) == 2 + length(var.data_buckets) + length(var.ai_artifact_buckets)
    )
    error_message = "Configure at least one data or AI artifact bucket; every managed bucket and the separately governed upstream access-log bucket must have a unique name."
  }
}

variable "ai_artifact_buckets" {
  description = "Create-only AI artifact buckets keyed by stable Terraform identity."
  type = map(object({
    name                       = string
    location                   = string
    kms_key_name               = string
    artifact_class             = string
    storage_class              = optional(string, "STANDARD")
    soft_delete_retention_days = optional(number, 30)
    retention_period_seconds   = optional(number, 7776000)
    publishers                 = set(string)
    readers                    = optional(set(string), [])
    labels                     = optional(map(string), {})
  }))
  default = {}

  validation {
    condition = alltrue([
      for bucket in values(var.ai_artifact_buckets) :
      length(bucket.name) >= 3 &&
      length(bucket.name) <= 63 &&
      can(regex("^[a-z0-9][a-z0-9._-]*[a-z0-9]$", bucket.name)) &&
      length(trimspace(bucket.location)) > 0 &&
      can(regex("^projects/[^/]+/locations/[^/]+/keyRings/[^/]+/cryptoKeys/[^/]+$", bucket.kms_key_name)) &&
      contains(["checkpoint", "model", "evaluation", "release-evidence"], bucket.artifact_class) &&
      contains(["STANDARD", "NEARLINE", "COLDLINE", "ARCHIVE"], bucket.storage_class) &&
      bucket.soft_delete_retention_days >= 7 &&
      bucket.soft_delete_retention_days <= 90 &&
      floor(bucket.soft_delete_retention_days) == bucket.soft_delete_retention_days &&
      bucket.retention_period_seconds >= 1 &&
      bucket.retention_period_seconds <= 3155760000 &&
      floor(bucket.retention_period_seconds) == bucket.retention_period_seconds &&
      length(bucket.publishers) > 0
    ])
    error_message = "AI artifact buckets require valid names, locations, CMEK, class, retention, soft delete, and at least one publisher."
  }

  validation {
    condition = alltrue(flatten([
      for bucket in values(var.ai_artifact_buckets) : [
        for member in setunion(bucket.publishers, bucket.readers) :
        !contains(["allUsers", "allAuthenticatedUsers"], member) && length(trimspace(member)) > 0
      ]
    ]))
    error_message = "AI artifact publishers and readers must be non-empty, non-public IAM members."
  }
}
