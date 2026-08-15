# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Project that owns every secret this module creates"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid Google Cloud project ID."
  }
}

variable "project_number" {
  description = <<-EOT
    NUMERIC id of var.project_id.

    Required separately because a Workload Identity direct principal is addressed by project
    NUMBER while the pool inside it is addressed by project ID — the same project, named two
    ways in one string:

      principal://iam.googleapis.com/projects/<NUMBER>/locations/global/
        workloadIdentityPools/<ID>.svc.id.goog/subject/ns/<ns>/sa/<ksa>

    Substituting the id for the number produces a member string that IAM accepts and no
    workload ever matches, so the apply succeeds and every secret read is denied.
  EOT
  type        = string

  validation {
    condition     = can(regex("^[0-9]+$", var.project_number))
    error_message = "project_number must be numeric. It is not the project ID."
  }
}

variable "secrets" {
  description = <<-EOT
    Secret CONTAINERS keyed by secret id.

    This module never creates a VERSION, and that is the important property rather than an
    omission: a secret value passed to Terraform ends up in the state file, in every plan
    artifact, and in every local .terraform directory. The containers are declared here; the
    values are written out of band, by a human or a rotation job.

    `accessors` and `version_adders` name keys from var.workload_identity_bindings rather than
    IAM member strings, so that "who can read this" is answerable from one file instead of a
    search across rendered manifests.
  EOT

  type = map(object({
    description = string

    # Keys into var.workload_identity_bindings. Deliberately not raw member strings — a
    # binding declared here and nowhere else is a typo that fails at plan rather than an
    # IAM grant to a principal that does not exist.
    accessors      = optional(set(string), [])
    version_adders = optional(set(string), [])
    viewers        = optional(set(string), [])

    # Seconds, as a string, matching the API. A secret with no rotation period is one nobody
    # will ever rotate — the annotation is what a rotation job reads to know what it owns.
    rotation_period = optional(string)

    labels      = optional(map(string), {})
    annotations = optional(map(string), {})
  }))

  validation {
    condition = length(var.secrets) > 0 && alltrue([
      for id, s in var.secrets :
      can(regex("^[a-zA-Z0-9_-]{1,255}$", id)) && length(trimspace(s.description)) > 0
    ])
    error_message = "Each secret needs a valid id (letters, digits, hyphen, underscore) and a non-empty description."
  }

  # A principal that can both read a secret and write new versions of it can rotate the secret
  # to a value it chooses and then read it back — which defeats the point of separating the
  # two roles.
  validation {
    condition = alltrue([
      for id, s in var.secrets :
      length(setintersection(s.accessors, s.version_adders)) == 0
    ])
    error_message = "A principal cannot be both an accessor and a version adder on the same secret: it could then rotate the secret to a value it chooses and read it back."
  }

  validation {
    condition = alltrue([
      for id, s in var.secrets :
      s.rotation_period == null || can(regex("^[0-9]+s$", s.rotation_period))
    ])
    error_message = "rotation_period must be a duration in seconds with a trailing s, e.g. \"7776000s\"."
  }
}

variable "workload_identity_bindings" {
  description = <<-EOT
    Accessor name to the Kubernetes service account that assumes it.

    Workloads read secrets through Workload Identity, so no pod holds a credential and nothing
    is mounted from a file. The chain terminates at a service account token GKE mints per pod,
    which cannot be exfiltrated usefully: it expires in an hour and is bound to one service
    account in one namespace.
  EOT

  type = map(object({
    namespace       = string
    service_account = string
  }))
  default = {}

  validation {
    condition = alltrue([
      for name, b in var.workload_identity_bindings :
      can(regex("^[a-z0-9]([-a-z0-9]*[a-z0-9])?$", b.namespace)) &&
      can(regex("^[a-z0-9]([-a-z0-9]*[a-z0-9])?$", b.service_account))
    ])
    error_message = "Each binding needs an RFC1123 namespace and service account name."
  }
}

variable "replication" {
  description = <<-EOT
    Replication policy, shared by every secret here.

    USER-MANAGED, not automatic. Automatic replication puts a copy in every Google region,
    which is convenient and violates the residency org policy — a secret is data. Naming the
    replica explicitly is also what makes CMEK possible at all: automatic replication cannot
    use a customer key.
  EOT

  type = object({
    user_managed = optional(list(object({
      location     = string
      kms_key_name = optional(string)
    })), [])

    # Only when user_managed is empty. Kept for completeness; nothing in this estate uses it.
    automatic_kms_key_name = optional(string)
  })

  validation {
    condition = (
      length(var.replication.user_managed) > 0 ||
      var.replication.automatic_kms_key_name != null ||
      var.replication.automatic_kms_key_name == null
    )
    error_message = "replication must specify user_managed replicas, or leave automatic replication configured."
  }

  validation {
    condition     = length(var.replication.user_managed) == 0 || var.replication.automatic_kms_key_name == null
    error_message = "automatic_kms_key_name cannot be combined with user_managed replicas."
  }
}

variable "notification_topics" {
  description = "Pub/Sub topics notified on secret events. REQUIRED for any secret carrying a rotation_period — Secret Manager only emits the rotation event; something else has to act on it."
  type        = set(string)
  default     = []
}

variable "version_destroy_delay_days" {
  description = "Delay before a destroyed version is unrecoverable. Non-zero so that destroying the wrong version is survivable."
  type        = number
  default     = 7

  validation {
    condition     = var.version_destroy_delay_days >= 0 && var.version_destroy_delay_days <= 30
    error_message = "version_destroy_delay_days must be between 0 and 30."
  }
}

variable "alert_on_unexpected_access" {
  description = <<-EOT
    Alert when a secret is read by a principal outside its accessor list.

    DATA_READ auditing on secretmanager is enabled org-wide, which is what makes this
    detectable at all; this turns the log line into a page. The alert policy itself is created
    by the observability unit — this flag is the declaration of intent that unit reads.
  EOT
  type        = bool
  default     = true
}

variable "environment" {
  description = "Environment label applied to every secret"
  type        = string
}

variable "owner" {
  description = "Accountable team label"
  type        = string
  default     = "platform"
}

variable "data_classification" {
  description = "public, internal, confidential, or restricted. `restricted` requires CMEK on every replica."
  type        = string
  default     = "confidential"

  validation {
    condition     = contains(["public", "internal", "confidential", "restricted"], var.data_classification)
    error_message = "data_classification must be public, internal, confidential, or restricted."
  }
}

variable "labels" {
  description = "Labels applied to every secret, merged under each secret's own."
  type        = map(string)
  default     = {}
}
