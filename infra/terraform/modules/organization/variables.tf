# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "organization_id" {
  description = "Numeric Google Cloud organization ID."
  type        = string

  validation {
    condition     = can(regex("^[0-9]+$", var.organization_id))
    error_message = "organization_id must contain only decimal digits."
  }
}

variable "tag_keys" {
  description = <<-EOT
    Organization-scoped tag taxonomy keyed by stable Terraform aliases. Each key
    contains an optional map of tag values, also keyed by stable aliases. Aliases
    are state addresses and must not be renamed without an explicit moved block.
  EOT
  type = map(object({
    short_name  = string
    description = optional(string, "")
    values = optional(map(object({
      short_name  = string
      description = optional(string, "")
    })), {})
  }))
  default = {}

  validation {
    condition = length(var.tag_keys) <= 100 && alltrue([
      for alias in keys(var.tag_keys) : can(regex("^[a-z][a-z0-9_-]{0,62}$", alias))
    ])
    error_message = "tag_keys supports at most 100 entries and every alias must match ^[a-z][a-z0-9_-]{0,62}$."
  }

  validation {
    condition = alltrue([
      for tag_key in values(var.tag_keys) :
      can(regex("^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$", tag_key.short_name)) &&
      length(tag_key.description) <= 256 &&
      length(tag_key.values) <= 100
    ])
    error_message = "Each tag key short_name must use 1-63 conservative tag characters, descriptions must be at most 256 characters, and a key may contain at most 100 values."
  }

  validation {
    condition = (
      length(distinct([for tag_key in values(var.tag_keys) : lower(tag_key.short_name)])) == length(var.tag_keys) &&
      sum([for tag_key in values(var.tag_keys) : length(tag_key.values)]) <= 500 &&
      alltrue([
        for tag_key in values(var.tag_keys) :
        length(distinct([for tag_value in values(tag_key.values) : lower(tag_value.short_name)])) == length(tag_key.values)
      ])
    )
    error_message = "Tag key short names and tag value short names within a key must be unique (case-insensitive), and the module supports at most 500 total values."
  }

  validation {
    condition = alltrue(flatten([
      for tag_key in values(var.tag_keys) : [
        for alias, tag_value in tag_key.values :
        can(regex("^[a-z][a-z0-9_-]{0,62}$", alias)) &&
        can(regex("^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$", tag_value.short_name)) &&
        length(tag_value.description) <= 256
      ]
    ]))
    error_message = "Every tag value alias and short_name must use the documented conservative format, and descriptions must be at most 256 characters."
  }
}

variable "tag_bindings" {
  description = <<-EOT
    Protected tag bindings keyed by stable aliases. parent must be a full numeric
    Cloud Resource Manager resource name. tag_value references a value declared in
    tag_keys using the form "<tag-key-alias>/<tag-value-alias>".
  EOT
  type = map(object({
    parent    = string
    tag_value = string
  }))
  default = {}

  validation {
    condition = length(var.tag_bindings) <= 500 && alltrue([
      for alias, binding in var.tag_bindings :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", alias)) &&
      can(regex("^//cloudresourcemanager\\.googleapis\\.com/(organizations|folders|projects)/[0-9]+$", binding.parent)) &&
      can(regex("^[a-z][a-z0-9_-]{0,62}/[a-z][a-z0-9_-]{0,62}$", binding.tag_value))
    ])
    error_message = "tag_bindings supports at most 500 entries; aliases and tag_value references must use stable alias syntax, and parent must be a full numeric organization, folder, or project resource name."
  }
}

variable "iam_grants" {
  description = <<-EOT
    Additive organization IAM grants keyed by stable aliases. Only groups, service
    accounts, workforce/workload principals, and principal sets are accepted.
    Basic roles, administrator roles, service-account impersonation roles, public
    principals, domains, deleted principals, and direct users are deliberately
    rejected at organization scope.
  EOT
  type = map(object({
    role   = string
    member = string
    condition = optional(object({
      title       = string
      description = optional(string, "")
      expression  = string
    }))
  }))
  default = {}

  validation {
    condition = length(var.iam_grants) <= 500 && alltrue([
      for alias in keys(var.iam_grants) : can(regex("^[a-z][a-z0-9_-]{0,62}$", alias))
    ])
    error_message = "iam_grants supports at most 500 entries and each alias must match ^[a-z][a-z0-9_-]{0,62}$."
  }

  validation {
    condition = alltrue([
      for grant in values(var.iam_grants) :
      can(regex("^(roles/[A-Za-z0-9_.]+|organizations/[0-9]+/roles/[A-Za-z0-9_.]+)$", grant.role)) &&
      (startswith(grant.role, "roles/") || startswith(grant.role, "organizations/${var.organization_id}/roles/")) &&
      !contains([
        "roles/owner",
        "roles/editor",
        "roles/viewer",
        "roles/iam.serviceAccountTokenCreator",
        "roles/iam.serviceAccountUser",
      ], grant.role) &&
      !endswith(lower(grant.role), "admin")
    ])
    error_message = "Organization grants must use predefined or organization custom roles; basic, administrator, and service-account impersonation roles are not accepted."
  }

  validation {
    condition = alltrue([
      for grant in values(var.iam_grants) :
      grant.member != "allUsers" &&
      grant.member != "allAuthenticatedUsers" &&
      !strcontains(grant.member, "*") &&
      can(regex("^(group:[^[:space:]]+@[^[:space:]]+|serviceAccount:[^[:space:]]+@[^[:space:]]+|principal://iam\\.googleapis\\.com/.+|principalSet://iam\\.googleapis\\.com/.+)$", grant.member))
    ])
    error_message = "Organization IAM members must be a non-wildcard group, serviceAccount, principal, or principalSet; public, user, domain, and deleted principals are rejected."
  }

  validation {
    condition = alltrue([
      for grant in values(var.iam_grants) :
      try(grant.condition == null, true) || try(
        length(trimspace(grant.condition.title)) > 0 &&
        length(grant.condition.title) <= 100 &&
        length(grant.condition.description) <= 256 &&
        length(trimspace(grant.condition.expression)) > 0 &&
        length(grant.condition.expression) <= 2048 &&
        !contains(["true", "1 == 1", "1==1"], lower(trimspace(grant.condition.expression))),
        false,
      )
    ])
    error_message = "IAM conditions require a non-empty title and non-trivial expression within the documented module limits."
  }
}
