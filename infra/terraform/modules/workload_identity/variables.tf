# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Project ID that owns the workload identity pool and dedicated Google service accounts."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid 6-30 character Google Cloud project ID."
  }
}

variable "project_number" {
  description = "Numeric project number used in principalSet identifiers. Do not pass the project ID."
  type        = string

  validation {
    condition     = can(regex("^[0-9]+$", var.project_number))
    error_message = "project_number must contain only decimal digits."
  }
}

variable "pool" {
  description = <<-EOT
    External workload identity pool. Set to null only when this module is used
    exclusively for GKE KSA bindings. One module instance intentionally manages
    one federation trust boundary.
  EOT
  type = object({
    pool_id      = string
    display_name = optional(string, "")
    description  = optional(string, "")
    disabled     = optional(bool, false)
  })
  default  = null
  nullable = true

  validation {
    condition = var.pool == null ? true : (
      can(regex("^[a-z0-9][a-z0-9-]{2,30}[a-z0-9]$", var.pool.pool_id)) &&
      !startswith(var.pool.pool_id, "gcp-") &&
      length(var.pool.display_name) <= 32 &&
      length(var.pool.description) <= 256
    )
    error_message = "pool_id must be 4-32 lowercase letters, digits, or hyphens (without the reserved gcp- prefix); display_name and description must fit provider limits."
  }

  validation {
    condition = (
      var.pool != null ||
      (length(var.oidc_providers) == 0 && length(var.federated_principal_sets) == 0)
    )
    error_message = "pool must be configured whenever oidc_providers or federated_principal_sets is non-empty."
  }
}

variable "oidc_providers" {
  description = <<-EOT
    OIDC providers keyed by stable aliases. Every provider requires explicit
    audiences, a google.subject mapping, at least one custom attribute mapping,
    and a condition that constrains a mapped custom attribute.
  EOT
  type = map(object({
    provider_id         = string
    display_name        = optional(string, "")
    description         = optional(string, "")
    disabled            = optional(bool, false)
    issuer_uri          = string
    allowed_audiences   = set(string)
    attribute_mapping   = map(string)
    attribute_condition = string
  }))
  default = {}

  validation {
    condition = length(var.oidc_providers) <= 20 && alltrue([
      for alias, provider in var.oidc_providers :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", alias)) &&
      can(regex("^[a-z0-9][a-z0-9-]{2,30}[a-z0-9]$", provider.provider_id)) &&
      !startswith(provider.provider_id, "gcp-") &&
      length(provider.display_name) <= 32 &&
      length(provider.description) <= 256 &&
      can(regex("^https://[^/?#]+(/[^?#]*)?$", provider.issuer_uri))
    ])
    error_message = "oidc_providers supports at most 20 entries; aliases, provider IDs, names, descriptions, and HTTPS issuer URIs must satisfy the documented module and provider limits."
  }

  validation {
    condition = alltrue([
      for provider in values(var.oidc_providers) :
      length(provider.allowed_audiences) >= 1 &&
      length(provider.allowed_audiences) <= 10 &&
      alltrue([
        for audience in provider.allowed_audiences :
        length(trimspace(audience)) > 0 && length(audience) <= 256 && !strcontains(audience, "*")
      ])
    ])
    error_message = "Each OIDC provider must declare 1-10 non-empty, non-wildcard allowed audiences of at most 256 characters."
  }

  validation {
    condition = alltrue([
      for provider in values(var.oidc_providers) :
      contains(keys(provider.attribute_mapping), "google.subject") &&
      strcontains(try(provider.attribute_mapping["google.subject"], ""), "assertion.") &&
      length(try(provider.attribute_mapping["google.subject"], "")) <= 2048 &&
      length([for key in keys(provider.attribute_mapping) : key if startswith(key, "attribute.")]) >= 1 &&
      length([for key in keys(provider.attribute_mapping) : key if startswith(key, "attribute.")]) <= 50 &&
      alltrue([
        for key, expression in provider.attribute_mapping :
        (contains(["google.subject", "google.groups"], key) || can(regex("^attribute\\.[a-z0-9_]{1,90}$", key))) &&
        length(trimspace(expression)) > 0 &&
        length(expression) <= 2048 &&
        strcontains(expression, "assertion.")
      ])
    ])
    error_message = "OIDC mappings require google.subject sourced from assertion.*, 1-50 valid custom attribute.* mappings, and non-empty assertion-based expressions within provider limits."
  }

  validation {
    condition = alltrue([
      for provider in values(var.oidc_providers) :
      length(trimspace(provider.attribute_condition)) >= 12 &&
      length(provider.attribute_condition) <= 4096 &&
      !contains(["true", "1 == 1", "1==1"], lower(trimspace(provider.attribute_condition))) &&
      !strcontains(provider.attribute_condition, "||") &&
      length([
        for key in keys(provider.attribute_mapping) : key
        if startswith(key, "attribute.") && strcontains(provider.attribute_condition, key)
      ]) >= 1 &&
      (
        strcontains(provider.attribute_condition, "==") ||
        strcontains(provider.attribute_condition, " in ") ||
        strcontains(provider.attribute_condition, ".startsWith(") ||
        strcontains(provider.attribute_condition, ".endsWith(")
      )
    ])
    error_message = "Each OIDC attribute_condition must be a non-trivial, non-OR CEL predicate that constrains at least one mapped custom attribute with equality, membership, startsWith, or endsWith."
  }
}

variable "service_accounts" {
  description = <<-EOT
    Dedicated Google service accounts keyed by stable aliases. project_roles are
    granted additively to the generated service-account member in project_id.
  EOT
  type = map(object({
    account_id    = string
    display_name  = optional(string, "")
    description   = optional(string, "")
    disabled      = optional(bool, false)
    project_roles = optional(set(string), [])
  }))
  default = {}

  validation {
    condition = length(var.service_accounts) <= 100 && alltrue([
      for alias, account in var.service_accounts :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", alias)) &&
      can(regex("^[a-z]([-a-z0-9]{4,28}[a-z0-9])$", account.account_id)) &&
      length(account.display_name) <= 100 &&
      length(account.description) <= 256 &&
      length(account.project_roles) <= 50
    ])
    error_message = "service_accounts supports at most 100 entries; aliases, RFC1035 account IDs, text fields, and the 50-role-per-account module limit must be valid."
  }

  validation {
    condition     = length(distinct([for account in values(var.service_accounts) : account.account_id])) == length(var.service_accounts)
    error_message = "Every service account must have a unique account_id."
  }

  validation {
    condition = alltrue(flatten([
      for account in values(var.service_accounts) : [
        for role in account.project_roles :
        (can(regex("^roles/[A-Za-z0-9_.]+$", role)) || can(regex("^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/roles/[A-Za-z0-9_.]+$", role))) &&
        (startswith(role, "roles/") || startswith(role, "projects/${var.project_id}/roles/")) &&
        !contains([
          "roles/owner",
          "roles/editor",
          "roles/viewer",
          "roles/iam.serviceAccountTokenCreator",
          "roles/iam.serviceAccountUser",
          "roles/iam.workloadIdentityUser",
        ], role) &&
        !endswith(lower(role), "admin")
      ]
    ]))
    error_message = "project_roles must be predefined or project custom roles; basic, administrator, and service-account impersonation roles are rejected."
  }
}

variable "federated_principal_sets" {
  description = <<-EOT
    Additive roles/iam.workloadIdentityUser grants from constrained external
    principalSets to dedicated service accounts. attribute omits the "attribute."
    prefix and must be mapped by the referenced provider.
  EOT
  type = map(object({
    service_account_key = string
    provider_key        = string
    attribute           = string
    value               = string
  }))
  default = {}

  validation {
    condition = length(var.federated_principal_sets) <= 500 && alltrue([
      for alias, grant in var.federated_principal_sets :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", alias)) &&
      can(regex("^[a-z][a-z0-9_-]{0,62}$", grant.service_account_key)) &&
      can(regex("^[a-z][a-z0-9_-]{0,62}$", grant.provider_key)) &&
      can(regex("^[a-z0-9_]{1,90}$", grant.attribute)) &&
      can(regex("^[A-Za-z0-9][A-Za-z0-9._~:/-]{0,255}$", grant.value)) &&
      !strcontains(grant.value, "*") &&
      !strcontains(grant.value, "..")
    ])
    error_message = "federated_principal_sets supports at most 500 entries; aliases/references/attributes must be valid, and values must be explicit non-wildcard URI-safe strings."
  }
}

variable "gke_ksa_bindings" {
  description = "Additive GKE KSA-to-dedicated-GSA Workload Identity bindings keyed by stable aliases."
  type = map(object({
    service_account_key = string
    namespace           = string
    ksa_name            = string
    gke_project_id      = optional(string)
  }))
  default = {}

  validation {
    condition = length(var.gke_ksa_bindings) <= 500 && alltrue([
      for alias, binding in var.gke_ksa_bindings :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", alias)) &&
      can(regex("^[a-z][a-z0-9_-]{0,62}$", binding.service_account_key)) &&
      can(regex("^[a-z0-9]([-a-z0-9]*[a-z0-9])?$", binding.namespace)) &&
      length(binding.namespace) <= 63 &&
      can(regex("^[a-z0-9]([-a-z0-9]*[a-z0-9])?$", binding.ksa_name)) &&
      length(binding.ksa_name) <= 63 &&
      try(binding.gke_project_id == null, true) || try(can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", binding.gke_project_id)), false)
    ])
    error_message = "gke_ksa_bindings supports at most 500 entries and requires stable aliases, DNS-1123 namespace/KSA names, and an optional valid GKE project ID."
  }
}
