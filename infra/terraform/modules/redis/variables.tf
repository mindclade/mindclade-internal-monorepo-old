# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Project that owns the Redis instance"
  type        = string
}

variable "name" {
  description = "Redis instance name"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,38}[a-z0-9]$", var.name))
    error_message = "name must use lowercase letters, digits, and hyphens and be at most 40 characters."
  }
}

variable "display_name" {
  description = "Human-readable instance name"
  type        = string
  default     = "Mindclade Redis"
}

variable "region" {
  description = "Region for the instance"
  type        = string

  validation {
    condition     = can(regex("^[a-z]+-[a-z]+[0-9]$", var.region))
    error_message = "region must look like us-central1."
  }
}

variable "primary_zone" {
  description = "Optional primary zone; null lets the service choose"
  type        = string
  default     = null

  validation {
    condition     = var.primary_zone == null || can(regex("^${var.region}-[a-z]$", coalesce(var.primary_zone, "")))
    error_message = "primary_zone must be null or a canonical zone in region, such as us-central1-a."
  }
}

variable "alternative_zone" {
  description = "Optional failover zone; must differ from primary_zone"
  type        = string
  default     = null

  validation {
    condition     = var.alternative_zone == null || can(regex("^${var.region}-[a-z]$", coalesce(var.alternative_zone, "")))
    error_message = "alternative_zone must be null or a canonical zone in region, such as us-central1-b."
  }
}

variable "authorized_network" {
  description = "Full self-link of the VPC allowed to connect"
  type        = string

  validation {
    condition     = can(regex("^projects/[^/]+/global/networks/[^/]+$", var.authorized_network)) || can(regex("^https://www.googleapis.com/compute/[^/]+/projects/[^/]+/global/networks/[^/]+$", var.authorized_network))
    error_message = "authorized_network must be a VPC resource name or self-link."
  }
}

variable "reserved_ip_range" {
  description = "Name of the private service access allocated range"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,61}[a-z0-9]$", var.reserved_ip_range))
    error_message = "reserved_ip_range must be an allocated range name."
  }
}

variable "memory_size_gb" {
  description = "Provisioned memory in GiB"
  type        = number
  default     = 5

  validation {
    condition     = var.memory_size_gb >= 5 && var.memory_size_gb <= 300 && floor(var.memory_size_gb) == var.memory_size_gb
    error_message = "memory_size_gb must be a whole number from 5 through the Memorystore maximum of 300."
  }
}

variable "redis_version" {
  description = "Memorystore Redis major/minor version"
  type        = string
  default     = "REDIS_7_2"

  validation {
    condition     = contains(["REDIS_7_0", "REDIS_7_2"], var.redis_version)
    error_message = "redis_version must be an explicitly reviewed supported Redis 7 version."
  }
}

variable "redis_configs" {
  description = "Runtime Redis configuration"
  type        = map(string)
  default = {
    "maxmemory-policy" = "allkeys-lru"
  }
}

variable "rdb_snapshot_period" {
  description = "RDB persistence frequency"
  type        = string
  default     = "SIX_HOURS"

  validation {
    condition     = contains(["ONE_HOUR", "SIX_HOURS", "TWELVE_HOURS", "TWENTY_FOUR_HOURS"], var.rdb_snapshot_period)
    error_message = "rdb_snapshot_period must be a supported Memorystore snapshot interval."
  }
}

variable "rdb_snapshot_start_time" {
  description = "Optional RFC3339 time anchoring the RDB schedule"
  type        = string
  default     = null

  validation {
    condition     = var.rdb_snapshot_start_time == null || can(timecmp(var.rdb_snapshot_start_time, var.rdb_snapshot_start_time))
    error_message = "rdb_snapshot_start_time must be null or a valid RFC3339 timestamp."
  }
}

variable "maintenance_day" {
  description = "Weekly maintenance day"
  type        = string
  default     = "SUNDAY"

  validation {
    condition     = contains(["MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY", "SUNDAY"], var.maintenance_day)
    error_message = "maintenance_day must be an uppercase weekday."
  }
}

variable "maintenance_hour_utc" {
  description = "Start hour for weekly maintenance in UTC"
  type        = number
  default     = 7

  validation {
    condition     = var.maintenance_hour_utc >= 0 && var.maintenance_hour_utc <= 23 && floor(var.maintenance_hour_utc) == var.maintenance_hour_utc
    error_message = "maintenance_hour_utc must be a whole hour from 0 through 23."
  }
}

variable "kms_key_name" {
  description = "Optional CryptoKey resource name; grant the Redis service agent access separately"
  type        = string
  default     = null

  validation {
    condition = (
      var.kms_key_name == null ||
      can(regex("^projects/[^/]+/locations/${var.region}/keyRings/[^/]+/cryptoKeys/[^/]+$", coalesce(var.kms_key_name, "")))
    )
    error_message = "kms_key_name must be null or a full CryptoKey resource name in the Redis region."
  }
}

variable "environment" {
  description = "Environment governance label"
  type        = string
}

variable "owner" {
  description = "Accountable team governance label"
  type        = string
}

variable "data_classification" {
  description = "Data-classification governance label"
  type        = string
  default     = "confidential"

  validation {
    condition     = contains(["internal", "confidential", "restricted"], var.data_classification)
    error_message = "Redis data must be internal, confidential, or restricted."
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
