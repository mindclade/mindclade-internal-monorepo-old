variable "project_id" {
  description = "Project that owns the secret metadata"
  type        = string
}

variable "secret_id" {
  description = "Secret identifier; never put a secret value in this field"
  type        = string

  validation {
    condition     = can(regex("^[A-Za-z0-9_-]{1,255}$", var.secret_id))
    error_message = "secret_id must use 1-255 letters, digits, underscores, or hyphens."
  }
}

variable "automatic_kms_key_name" {
  description = "Optional global CryptoKey used with automatic replication"
  type        = string
  default     = null

  validation {
    condition = (
      var.automatic_kms_key_name == null ||
      can(regex("^projects/[^/]+/locations/global/keyRings/[^/]+/cryptoKeys/[^/]+$", coalesce(var.automatic_kms_key_name, "")))
    )
    error_message = "automatic_kms_key_name must be null or a full CryptoKey resource name in the global location."
  }
}

variable "user_managed_replicas" {
  description = "Explicit replica locations mapped to an optional regional CryptoKey; an empty map selects automatic replication"
  type        = map(string)
  default     = {}

  validation {
    condition     = length(var.user_managed_replicas) == 0 || length(var.user_managed_replicas) >= 2
    error_message = "Use automatic replication or at least two explicit user-managed replica locations."
  }

  validation {
    condition = alltrue([
      for location, kms_key_name in var.user_managed_replicas :
      can(regex("^[a-z0-9-]+$", location)) && (
        kms_key_name == null ||
        can(regex("^projects/[^/]+/locations/${location}/keyRings/[^/]+/cryptoKeys/[^/]+$", coalesce(kms_key_name, "")))
      )
    ])
    error_message = "Each user-managed replica needs a valid location and any CMEK must be a full CryptoKey resource name in that location."
  }
}

variable "version_destroy_delay_days" {
  description = "Recovery delay between a version-destroy request and permanent destruction"
  type        = number
  default     = 7

  validation {
    condition     = var.version_destroy_delay_days >= 1 && var.version_destroy_delay_days <= 30 && floor(var.version_destroy_delay_days) == var.version_destroy_delay_days
    error_message = "version_destroy_delay_days must be a whole number from 1 through 30."
  }
}

variable "notification_topics" {
  description = "Pub/Sub topic resource names for control-plane and rotation notifications"
  type        = set(string)
  default     = []

  validation {
    condition     = length(var.notification_topics) <= 10 && alltrue([for topic in var.notification_topics : can(regex("^projects/[^/]+/topics/[^/]+$", topic))])
    error_message = "Provide at most 10 full Pub/Sub topic resource names."
  }
}

variable "rotation" {
  description = "Optional notification schedule; it does not rotate payloads without an external handler"
  type = object({
    next_rotation_time = string
    period_days        = number
  })
  default = null

  validation {
    condition = var.rotation == null || (
      can(regex("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\\.[0-9]+)?Z$", var.rotation.next_rotation_time)) &&
      var.rotation.period_days >= 1 && var.rotation.period_days <= 36500 && floor(var.rotation.period_days) == var.rotation.period_days
    )
    error_message = "rotation needs an RFC3339 UTC next_rotation_time and a whole period_days from 1 through 36500."
  }
}

variable "accessor_members" {
  description = "Runtime IAM members allowed to access secret payload versions"
  type        = set(string)
  default     = []

  validation {
    condition     = alltrue([for member in var.accessor_members : !contains(["allUsers", "allAuthenticatedUsers"], member)])
    error_message = "Public secret access is forbidden."
  }
}

variable "version_adder_members" {
  description = "Rotation IAM members allowed to add, but not read, secret versions"
  type        = set(string)
  default     = []

  validation {
    condition     = alltrue([for member in var.version_adder_members : !contains(["allUsers", "allAuthenticatedUsers"], member)])
    error_message = "Public version creation is forbidden."
  }
}

variable "viewer_members" {
  description = "Operators allowed to view secret metadata but not payloads"
  type        = set(string)
  default     = []

  validation {
    condition     = alltrue([for member in var.viewer_members : !contains(["allUsers", "allAuthenticatedUsers"], member)])
    error_message = "Public secret metadata access is forbidden."
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
  default     = "restricted"

  validation {
    condition     = contains(["confidential", "restricted"], var.data_classification)
    error_message = "Secrets must be classified confidential or restricted."
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

variable "annotations" {
  description = "Non-sensitive operator metadata; never place payloads or credentials here"
  type        = map(string)
  default     = {}

  validation {
    condition = (
      length(var.annotations) <= 100 &&
      sum(concat([0], [for key, value in var.annotations : length(key) + length(value)])) < 16384 &&
      alltrue([
        for key, value in var.annotations :
        can(regex("^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,61}[A-Za-z0-9])?$", key)) &&
        can(regex("^[\\x20-\\x7E]*$", value))
      ])
    )
    error_message = "annotations are limited to 100 ASCII metadata pairs, 1-63 character API-valid keys, printable ASCII values, and less than 16 KiB total."
  }
}
