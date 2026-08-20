# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

variable "parent" {
  description = "Organization or folder covered by the aggregated audit sink."
  type        = string

  validation {
    condition     = can(regex("^(organizations|folders)/[0-9]{1,25}$", var.parent))
    error_message = "parent must be organizations/<numeric id> or folders/<numeric id>."
  }
}

variable "project_id" {
  description = "Dedicated central logging project that owns the archive bucket."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid Google Cloud project ID."
  }
}

variable "environment" {
  description = "Environment governance label for the central archive."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.environment))
    error_message = "environment must be a valid Google Cloud label value."
  }
}

variable "owner" {
  description = "Accountable security or platform team label."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.owner))
    error_message = "owner must be a valid Google Cloud label value."
  }
}

variable "storage_service_agent_email" {
  description = "Google-managed Cloud Storage service-agent email for project_id, used to report the required archive CMEK grant."
  type        = string

  validation {
    condition     = can(regex("^service-[0-9]+@gs-project-accounts[.]iam[.]gserviceaccount[.]com$", var.storage_service_agent_email))
    error_message = "storage_service_agent_email must be service-<project-number>@gs-project-accounts.iam.gserviceaccount.com."
  }
}

variable "sink_name" {
  description = "Stable aggregated sink name."
  type        = string
  default     = "central-audit-archive"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,98}[a-z0-9]$", var.sink_name))
    error_message = "sink_name must be a 3-100 character lowercase sink name."
  }
}

variable "bucket_name" {
  description = "Globally unique archive bucket name."
  type        = string

  validation {
    condition     = length(var.bucket_name) >= 3 && length(var.bucket_name) <= 63 && can(regex("^[a-z0-9][a-z0-9._-]*[a-z0-9]$", var.bucket_name))
    error_message = "bucket_name must be a valid 3-63 character Cloud Storage bucket name."
  }
}

variable "location" {
  description = "Archive bucket region, dual-region, or multi-region."
  type        = string

  validation {
    condition     = length(trimspace(var.location)) > 0
    error_message = "location must not be empty."
  }
}

variable "kms_key_name" {
  description = "Full CMEK resource name in a location compatible with the archive bucket."
  type        = string

  validation {
    condition     = can(regex("^projects/[^/]+/locations/[^/]+/keyRings/[^/]+/cryptoKeys/[^/]+$", var.kms_key_name))
    error_message = "kms_key_name must be a full CryptoKey resource name."
  }
}

variable "retention_days" {
  description = "Irreversible minimum audit-object retention once the bucket is created and locked."
  type        = number
  default     = 2555

  validation {
    condition     = var.retention_days >= 365 && var.retention_days <= 36500 && floor(var.retention_days) == var.retention_days
    error_message = "retention_days must be a whole number from 365 through 36500."
  }
}

variable "soft_delete_retention_days" {
  description = "Additional recovery window for deleted objects, from 7 through 90 days."
  type        = number
  default     = 90

  validation {
    condition     = var.soft_delete_retention_days >= 7 && var.soft_delete_retention_days <= 90 && floor(var.soft_delete_retention_days) == var.soft_delete_retention_days
    error_message = "soft_delete_retention_days must be a whole number from 7 through 90."
  }
}

variable "retention_lock_confirmation" {
  description = "Required exact acknowledgement of the irreversible Bucket Lock operation."
  type        = string
  sensitive   = true

  validation {
    condition     = var.retention_lock_confirmation == "LOCKING A CLOUD STORAGE RETENTION POLICY IS IRREVERSIBLE"
    error_message = "Set the exact irreversible retention-lock acknowledgement after security, legal, recovery, and cost approval."
  }
}

variable "destination_project_default_retention_days" {
  description = "Retention for only the central destination project's _Default bucket; 0 leaves it unmanaged."
  type        = number
  default     = 0

  validation {
    condition     = var.destination_project_default_retention_days >= 0 && var.destination_project_default_retention_days <= 3650 && floor(var.destination_project_default_retention_days) == var.destination_project_default_retention_days
    error_message = "destination_project_default_retention_days must be a whole number from 0 through 3650."
  }
}

variable "labels" {
  description = "Additional archive labels; security baseline labels take precedence."
  type        = map(string)
  default     = {}

  validation {
    condition = length(var.labels) <= 59 && alltrue([
      for key, value in var.labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
      can(regex("^$|^[a-z0-9][a-z0-9_-]{0,62}$", value))
    ])
    error_message = "labels must contain at most 59 valid lowercase pairs."
  }
}
