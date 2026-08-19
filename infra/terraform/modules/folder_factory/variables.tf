# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "parent" {
  description = "Resource under which every folder is created, as organizations/<id> or folders/<id>"
  type        = string

  validation {
    condition     = can(regex("^(organizations|folders)/[0-9]{1,25}$", var.parent))
    error_message = "parent must be organizations/<numeric id> or folders/<numeric id>."
  }
}

variable "folders" {
  description = <<-EOT
    Folders to create, keyed by a stable short name. The key is the identity: renaming it
    destroys and recreates the folder, taking every IAM binding and org policy attached to
    it, so the display name is what changes when a folder is renamed.
  EOT
  type = map(object({
    display_name        = string
    deletion_protection = optional(bool, true)
  }))

  validation {
    condition     = alltrue([for k, v in var.folders : can(regex("^[a-z][a-z0-9-]{0,28}[a-z0-9]$", k))])
    error_message = "Each folder key must be a 2-30 character lowercase name."
  }

  validation {
    condition     = alltrue([for k, v in var.folders : length(v.display_name) >= 3 && length(v.display_name) <= 30])
    error_message = "display_name must be 3-30 characters, which is the Google Cloud limit."
  }
}

variable "billing_account" {
  description = <<-EOT
    Billing account the folder budgets are raised against. Required whenever folder_budgets
    is non-empty — a budget has no meaning without one, and Google reports the omission as a
    permission error rather than a missing field.
  EOT
  type        = string
  default     = ""

  validation {
    condition     = var.billing_account == "" || can(regex("^[0-9A-F]{6}-[0-9A-F]{6}-[0-9A-F]{6}$", var.billing_account))
    error_message = "billing_account must be XXXXXX-XXXXXX-XXXXXX in uppercase hex."
  }
}

variable "folder_budgets" {
  description = <<-EOT
    Monthly budget in whole currency units, keyed by the same short name as folders. A folder
    with no entry has no budget, which is a decision rather than a default: an unbudgeted
    folder is where a runaway spends unnoticed.
  EOT
  type        = map(number)
  default     = {}

  validation {
    condition     = alltrue([for k, v in var.folder_budgets : v > 0])
    error_message = "Every budget must be greater than zero."
  }
}

variable "budget_currency" {
  description = "ISO 4217 currency for folder budgets. Must match the billing account's currency."
  type        = string
  default     = "USD"

  validation {
    condition     = can(regex("^[A-Z]{3}$", var.budget_currency))
    error_message = "budget_currency must be a three-letter ISO 4217 code."
  }
}

variable "budget_threshold_percents" {
  description = <<-EOT
    Fractions of the budget at which an alert fires. 1.0 is included deliberately: an alert
    that only fires at 90% tells nobody the month actually went over.
  EOT
  type        = list(number)
  default     = [0.5, 0.9, 1.0]

  validation {
    condition     = length(var.budget_threshold_percents) > 0
    error_message = "At least one alert threshold is required; a budget with no alert is a number nobody sees."
  }
}

variable "budget_monitoring_channels" {
  description = "Monitoring notification channel IDs alerted on every folder budget threshold."
  type        = list(string)
  default     = []
}

variable "labels" {
  description = "Non-sensitive labels applied to budgets. Folders do not carry labels in Google Cloud."
  type        = map(string)
  default     = {}
}
