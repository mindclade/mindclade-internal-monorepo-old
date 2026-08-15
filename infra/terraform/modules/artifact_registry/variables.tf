variable "project_id" {
  description = "Google Cloud project that owns the repository"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid 6-30 character Google Cloud project ID."
  }
}

variable "location" {
  description = "Regional or multi-regional Artifact Registry location"
  type        = string

  validation {
    condition     = can(regex("^(asia|europe|us|[a-z][a-z0-9-]*[0-9])$", var.location))
    error_message = "location must be a lowercase Artifact Registry region or the asia, europe, or us multi-region."
  }
}

variable "repository_id" {
  description = "Stable Docker repository ID within the selected project and location"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,61}[a-z0-9]$", var.repository_id))
    error_message = "repository_id must contain 3-63 lowercase letters, digits, or hyphens, beginning with a letter and ending with a letter or digit."
  }
}

variable "description" {
  description = "Non-sensitive repository purpose shown in Artifact Registry"
  type        = string
  default     = "Managed private Docker artifacts."

  validation {
    condition     = length(trimspace(var.description)) >= 4 && length(var.description) <= 1024
    error_message = "description must contain 4-1024 characters and must not contain sensitive data."
  }
}

variable "environment" {
  description = "Deployment environment governance label"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.environment))
    error_message = "environment must be a valid non-empty Google Cloud label value."
  }
}

variable "owner" {
  description = "Accountable team governance label"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.owner))
    error_message = "owner must be a valid non-empty Google Cloud label value."
  }
}

variable "data_classification" {
  description = "Repository data-classification governance label"
  type        = string
  default     = "internal"

  validation {
    condition     = contains(["public", "internal", "confidential", "restricted"], var.data_classification)
    error_message = "data_classification must be public, internal, confidential, or restricted."
  }
}

variable "labels" {
  description = "Additional repository labels; baseline governance labels take precedence"
  type        = map(string)
  default     = {}

  validation {
    condition = length(var.labels) <= 60 && alltrue([
      for key, value in var.labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
      can(regex("^$|^[a-z0-9][a-z0-9_-]{0,62}$", value))
    ])
    error_message = "labels must leave room for baseline labels and use valid Google Cloud label keys and values."
  }
}

variable "kms_key_name" {
  description = "Optional CMEK crypto-key resource name; the key location must match the repository"
  type        = string
  default     = null
  nullable    = true

  validation {
    condition = var.kms_key_name == null || can(regex(
      "^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/locations/[a-z0-9-]+/keyRings/[A-Za-z0-9_-]{1,63}/cryptoKeys/[A-Za-z0-9_-]{1,63}$",
      var.kms_key_name,
    ))
    error_message = "kms_key_name must be null or a complete Cloud KMS crypto-key resource name."
  }
}

variable "cleanup_policy_dry_run" {
  description = "Keep cleanup policies in log-only dry-run mode; defaults to the safe rollout state"
  type        = bool
  default     = true
}

variable "cleanup_activation_approved" {
  description = "Explicit acknowledgement required before cleanup_policy_dry_run can be disabled"
  type        = bool
  default     = false
}

variable "untagged_retention_days" {
  description = "Age in days after which untagged versions become cleanup candidates"
  type        = number
  default     = 30

  validation {
    condition     = floor(var.untagged_retention_days) == var.untagged_retention_days && var.untagged_retention_days >= 7 && var.untagged_retention_days <= 3650
    error_message = "untagged_retention_days must be a whole number from 7 through 3650."
  }
}

variable "minimum_versions_to_keep" {
  description = "Minimum most-recent versions retained for each package"
  type        = number
  default     = 20

  validation {
    condition     = floor(var.minimum_versions_to_keep) == var.minimum_versions_to_keep && var.minimum_versions_to_keep >= 1 && var.minimum_versions_to_keep <= 10000
    error_message = "minimum_versions_to_keep must be a whole number from 1 through 10000."
  }
}
