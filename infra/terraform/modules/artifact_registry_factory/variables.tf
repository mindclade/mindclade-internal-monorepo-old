# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Project that owns the repository collection."
  type        = string
}

variable "location" {
  description = "Artifact Registry location shared by the collection."
  type        = string
}

variable "encryption_key" {
  description = "CMEK used by every repository; its owning state must grant the Artifact Registry service agent."
  type        = string
}

variable "repositories" {
  description = "Repositories keyed by stable Terraform identity."
  type = map(object({
    format      = string
    description = string
    docker_config = optional(object({
      immutable_tags = optional(bool, true)
    }))
    cleanup_policies = optional(map(object({
      action               = string
      condition_state      = optional(string)
      older_than           = optional(string)
      most_recent_versions = optional(number)
    })), {})
  }))

  validation {
    condition = length(var.repositories) > 0 && alltrue([
      for repository_id, repository in var.repositories :
      can(regex("^[a-z][a-z0-9-]{1,61}[a-z0-9]$", repository_id)) &&
      contains(["DOCKER", "GO", "MAVEN", "NPM", "PYTHON"], repository.format) &&
      (repository.format == "DOCKER" || repository.docker_config == null) &&
      alltrue([
        for policy in values(repository.cleanup_policies) :
        contains(["DELETE", "KEEP"], policy.action) &&
        ((policy.condition_state != null || policy.older_than != null) != (policy.most_recent_versions != null))
      ])
    ])
    error_message = "Repositories require stable IDs, supported formats, format-compatible Docker settings, and exactly one cleanup selector per policy."
  }
}

variable "enable_vulnerability_scanning" {
  description = "Require inherited Artifact Analysis scanning for Docker repositories."
  type        = bool
  default     = true
}

variable "labels" {
  description = "Governance labels applied to every repository."
  type        = map(string)
  default     = {}
}
