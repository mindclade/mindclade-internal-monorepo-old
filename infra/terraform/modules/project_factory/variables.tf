# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "billing_account" {
  type        = string
  description = "Billing account ID attached to every project."
  validation {
    condition     = can(regex("^(?:billingAccounts/)?[0-9A-F]{6}-[0-9A-F]{6}-[0-9A-F]{6}$", var.billing_account))
    error_message = "billing_account must be an exact Google billing account ID."
  }
}

variable "folder_id" {
  type        = string
  description = "Parent folder resource name shared by the project set."
  validation {
    condition     = can(regex("^folders/[0-9]+$", var.folder_id))
    error_message = "folder_id must use folders/<numeric-id>."
  }
}

variable "projects" {
  description = "Projects keyed by a stable short name used by downstream state contracts."
  type = map(object({
    project_id                 = string
    name                       = string
    services                   = set(string)
    lien                       = optional(bool, false)
    shared_vpc_host            = optional(bool, false)
    shared_vpc_service_project = optional(bool, false)
    monitoring_scope_host      = optional(bool, false)
  }))
  validation {
    condition = length(var.projects) > 0 && alltrue([
      for _, project in var.projects :
      can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", project.project_id)) &&
      length(trimspace(project.name)) > 0 && length(project.services) > 0 &&
      !(project.shared_vpc_host && project.shared_vpc_service_project)
    ])
    error_message = "Projects require valid IDs, names, services, and mutually exclusive Shared VPC roles."
  }
  validation {
    condition = (
      length([for _, project in var.projects : project if project.shared_vpc_host]) <= 1 &&
      length([for _, project in var.projects : project if project.monitoring_scope_host]) <= 1 &&
      (length([for _, project in var.projects : project if project.shared_vpc_service_project]) == 0 ||
      length([for _, project in var.projects : project if project.shared_vpc_host]) == 1)
    )
    error_message = "A set may have at most one network and metrics-scope host; service projects require exactly one network host."
  }
}

variable "deletion_policy" {
  type        = string
  default     = "PREVENT"
  description = "Fail-closed project lifecycle; only PREVENT is supported."
  validation {
    condition     = var.deletion_policy == "PREVENT"
    error_message = "Project factory deletion_policy must remain PREVENT."
  }
}

variable "budget_amount" {
  type        = number
  default     = null
  nullable    = true
  description = "Optional aggregate monthly USD budget for this project set."
  validation {
    condition     = var.budget_amount == null || var.budget_amount > 0
    error_message = "budget_amount must be null or positive."
  }
}

variable "extra_services" {
  type        = set(string)
  default     = []
  description = "Additional APIs enabled only in the declared monitoring-scope host."
}

variable "labels" {
  type        = map(string)
  default     = {}
  description = "Labels merged with the child project module's mandatory ownership labels."
}
