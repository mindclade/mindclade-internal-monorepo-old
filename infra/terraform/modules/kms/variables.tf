variable "project_id" {
  description = "Google Cloud project that owns the key ring"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid Google Cloud project ID."
  }
}

variable "location" {
  description = "Immutable Cloud KMS location for the key ring"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,62}$", var.location))
    error_message = "location must be a valid lowercase Cloud KMS location identifier."
  }
}

variable "key_ring_name" {
  description = "Stable name for the Cloud KMS key ring"
  type        = string

  validation {
    condition     = can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", var.key_ring_name))
    error_message = "key_ring_name must be a valid 1-63 character lowercase name."
  }
}

variable "labels" {
  description = "Non-sensitive labels merged into every CryptoKey"
  type        = map(string)
  default     = {}

  validation {
    condition = length(var.labels) <= 63 && alltrue([
      for key, value in var.labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
      can(regex("^[a-z0-9_-]{0,63}$", value))
    ])
    error_message = "labels must leave room for managed-by and contain valid lowercase Google Cloud label pairs."
  }
}

variable "keys" {
  description = "Symmetric encryption keys keyed by stable CryptoKey name"
  type = map(object({
    rotation_period_seconds            = optional(number, 7776000)
    destroy_scheduled_duration_seconds = optional(number, 2592000)
    protection_level                   = optional(string, "SOFTWARE")
    labels                             = optional(map(string), {})
  }))

  validation {
    condition = length(var.keys) > 0 && alltrue([
      for name, key in var.keys :
      can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", name)) &&
      key.rotation_period_seconds >= 86400 &&
      key.rotation_period_seconds <= 7776000 &&
      floor(key.rotation_period_seconds) == key.rotation_period_seconds &&
      key.destroy_scheduled_duration_seconds >= 86400 &&
      key.destroy_scheduled_duration_seconds <= 10368000 &&
      floor(key.destroy_scheduled_duration_seconds) == key.destroy_scheduled_duration_seconds &&
      contains(["SOFTWARE", "HSM"], key.protection_level) &&
      length(key.labels) <= 63 && alltrue([
        for label_key, label_value in key.labels :
        can(regex("^[a-z][a-z0-9_-]{0,62}$", label_key)) &&
        can(regex("^[a-z0-9_-]{0,63}$", label_value))
      ])
    ])
    error_message = "keys require valid names, 1-90 day rotations, 1-120 day destruction delays, SOFTWARE or HSM protection, and valid labels."
  }
}
