# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" { type = string }
variable "location" { type = string }
variable "encryption_key" { type = string }
variable "uniform_bucket_level_access" {
  type    = bool
  default = true
}
variable "public_access_prevention" {
  type    = string
  default = "enforced"
}
variable "versioning" {
  type    = bool
  default = true
}
variable "soft_delete_retention_seconds" {
  type    = number
  default = 604800
}
variable "retention_lock_confirmation" {
  description = "Exact acknowledgement required when any bucket declares retention_days."
  type        = string
  default     = null
  sensitive   = true
}
variable "lifecycle_rules" {
  description = "Collection-wide default lifecycle rules."
  type = list(object({
    condition = object({
      age                        = optional(number)
      days_since_noncurrent_time = optional(number)
      matches_storage_class      = optional(list(string))
      num_newer_versions         = optional(number)
      with_state                 = optional(string)
    })
    action = object({
      type          = string
      storage_class = optional(string)
    })
  }))
  default = []
}
variable "buckets" {
  description = "Buckets keyed by stable Terraform identity."
  type = map(object({
    name                          = string
    location                      = optional(string)
    hierarchical_namespace        = optional(bool, false)
    versioning                    = optional(bool)
    soft_delete_retention_seconds = optional(number)
    retention_days                = optional(number)
    lifecycle_rules = optional(list(object({
      condition = object({
        age                        = optional(number)
        days_since_noncurrent_time = optional(number)
        matches_storage_class      = optional(list(string))
        num_newer_versions         = optional(number)
        with_state                 = optional(string)
      })
      action = object({
        type          = string
        storage_class = optional(string)
      })
    })))
  }))
  validation {
    condition = length(var.buckets) > 0 && alltrue([
      for bucket in values(var.buckets) :
      length(bucket.name) >= 3 && length(bucket.name) <= 63 &&
      (bucket.soft_delete_retention_seconds == null || bucket.soft_delete_retention_seconds == 0 || (bucket.soft_delete_retention_seconds >= 604800 && bucket.soft_delete_retention_seconds <= 7776000)) &&
      (bucket.retention_days == null || (bucket.retention_days >= 1 && floor(bucket.retention_days) == bucket.retention_days))
    ])
    error_message = "Buckets require valid names, supported soft-delete windows, and whole-day retention."
  }
}
variable "bucket_iam_members" {
  description = "Additive, bucket-scoped read grants keyed by stable Terraform identity."
  type = map(object({
    bucket_key = string
    role       = string
    member     = string
  }))
  default = {}
  validation {
    condition = alltrue([
      for grant in values(var.bucket_iam_members) : contains(keys(var.buckets), grant.bucket_key)
    ])
    error_message = "Every bucket IAM member must reference a bucket key declared by this module."
  }
  validation {
    condition = alltrue([
      for grant in values(var.bucket_iam_members) :
      grant.role == "roles/storage.objectViewer" &&
      can(regex("^serviceAccount:[a-z0-9-]+@[a-z0-9-]+\\.iam\\.gserviceaccount\\.com$", grant.member))
    ])
    error_message = "Bucket IAM members are limited to service-account principals with roles/storage.objectViewer."
  }
}
variable "deny_policies" {
  description = "Project-attached deny policies that this module automatically scopes to its sole managed bucket."
  type = map(object({
    display_name = string
    rules = list(object({
      denied_principals    = set(string)
      denied_permissions   = set(string)
      exception_principals = optional(set(string), [])
    }))
  }))
  default = {}
  validation {
    condition     = length(var.deny_policies) == 0 || length(var.buckets) == 1
    error_message = "Deny policies are accepted only for a single-bucket collection so their resource condition cannot be ambiguous."
  }
}
variable "labels" {
  type    = map(string)
  default = {}
}
