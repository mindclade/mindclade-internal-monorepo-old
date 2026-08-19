# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Globally unique GCP project ID"
  type        = string

  validation {
    condition = (
      can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id)) &&
      !anytrue([
        for restricted in ["google", "ssl", "null", "undefined"] :
        strcontains(var.project_id, restricted)
      ])
    )
    error_message = "project_id must be 6-30 lowercase letters, digits, or hyphens, begin with a letter, end with a letter or digit, and omit the restricted google, ssl, null, and undefined strings."
  }
}

variable "project_name" {
  description = "Human-readable GCP project name"
  type        = string

  validation {
    condition     = can(regex("^[-A-Za-z0-9'! ]{3,29}[A-Za-z0-9]$", var.project_name))
    error_message = "project_name must contain 4-30 letters, digits, single quotes, hyphens, spaces, or exclamation points and end with a letter or digit."
  }
}

variable "folder_id" {
  description = "Parent folder resource name, for example folders/123456789012"
  type        = string

  validation {
    condition     = can(regex("^folders/[0-9]{6,32}$", var.folder_id))
    error_message = "folder_id must be a folder resource name such as folders/123456789012."
  }
}

variable "billing_account_id" {
  description = "Billing account attached to the project"
  type        = string

  validation {
    condition     = can(regex("^[0-9A-F]{6}-[0-9A-F]{6}-[0-9A-F]{6}$", var.billing_account_id))
    error_message = "billing_account_id must use the form ABCDEF-012345-6789AB."
  }
}

variable "environment" {
  description = "Environment label applied to the project"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.environment))
    error_message = "environment must be a valid non-empty GCP label value."
  }
}

variable "owner" {
  description = "Accountable team label applied to the project"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.owner))
    error_message = "owner must be a valid non-empty GCP label value."
  }
}

variable "data_classification" {
  description = "Data-classification label applied to the project"
  type        = string
  default     = "internal"

  validation {
    condition     = contains(["public", "internal", "confidential", "restricted"], var.data_classification)
    error_message = "data_classification must be public, internal, confidential, or restricted."
  }
}

variable "resource_lifecycle" {
  description = "Lifecycle label applied to the project"
  type        = string
  default     = "persistent"

  validation {
    condition     = contains(["ephemeral", "persistent"], var.resource_lifecycle)
    error_message = "resource_lifecycle must be ephemeral or persistent."
  }
}

variable "labels" {
  description = "Additional GCP labels; baseline governance labels take precedence"
  type        = map(string)
  default     = {}

  validation {
    condition = length(var.labels) <= 59 && alltrue([
      for key, value in var.labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
      can(regex("^$|^[a-z0-9][a-z0-9_-]{0,62}$", value))
    ])
    error_message = "labels must contain at most 59 valid lowercase pairs, leaving room for project governance labels."
  }
}

variable "activate_apis" {
  description = "Google APIs to enable without disabling them during destroy"
  type        = set(string)
  default     = []

  validation {
    condition     = alltrue([for service in var.activate_apis : can(regex("^[a-z0-9.-]+\\.googleapis\\.com$", service))])
    error_message = "Every activate_apis entry must be a googleapis.com service name."
  }
}

variable "monthly_budget_usd" {
  description = "Optional whole-dollar monthly project budget; null disables budget creation"
  type        = number
  default     = null

  validation {
    condition     = var.monthly_budget_usd == null ? true : (var.monthly_budget_usd >= 1 && floor(var.monthly_budget_usd) == var.monthly_budget_usd)
    error_message = "monthly_budget_usd must be null or a positive whole-dollar amount."
  }
}

variable "budget_notification_channels" {
  description = "Monitoring notification channel resource names for budget updates"
  type        = set(string)
  default     = []

  validation {
    condition = (
      length(var.budget_notification_channels) <= 5 &&
      alltrue([
        for channel in var.budget_notification_channels :
        can(regex("^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/notificationChannels/[^/]+$|^projects/[0-9]{6,32}/notificationChannels/[^/]+$", channel))
      ])
    )
    error_message = "budget_notification_channels must contain at most five full projects/{project}/notificationChannels/{channel} resource names."
  }
}

variable "enable_project_level_budget_recipients" {
  description = "Notify project owners through the billing budget"
  type        = bool
  default     = true
}

variable "tag_value_names" {
  description = "Namespaced tag value resource names to bind to the project"
  type        = set(string)
  default     = []

  validation {
    condition     = alltrue([for tag_value in var.tag_value_names : can(regex("^tagValues/[0-9]+$", tag_value))])
    error_message = "Every tag_value_names entry must use the form tagValues/123456789012."
  }
}

variable "shared_vpc_host_project_id" {
  description = "Shared VPC host project to attach this project to as a service project; null leaves it unattached"
  type        = string
  default     = null

  validation {
    condition     = var.shared_vpc_host_project_id == null ? true : can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.shared_vpc_host_project_id))
    error_message = "shared_vpc_host_project_id must be null or a valid project ID."
  }
}

variable "remove_default_service_account" {
  description = "Delete the default compute service account, which otherwise holds roles/editor on its own project"
  type        = bool
  default     = false
}
