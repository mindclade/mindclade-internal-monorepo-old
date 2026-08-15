# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Project holding the IAP-protected backend services."
  type        = string
}

variable "backend_services" {
  description = <<-EOT
    Compute backend service NAMES, keyed by the Kubernetes Service they came from.

    GKE Gateway generates these names; they are not the Kubernetes Service names. Read them
    back after the Gateway is programmed:

      gcloud compute backend-services list --format='table(name, description)'

    The description carries the originating Kubernetes Service, which is what makes the
    mapping legible rather than a guess at a hashed name.

    Only the IAP-ENABLED backends belong here. studio-embed and go-vanity are deliberately
    absent — they carry no IAP policy, so a binding on them would grant a role against a
    control that is not there.
  EOT
  type        = map(string)

  validation {
    condition     = length(var.backend_services) > 0
    error_message = "No backend services given; this module would create no binding and report success."
  }
}

variable "accessor_groups" {
  description = <<-EOT
    Google groups permitted to complete an IAP sign-in.

    GROUPS ONLY. A binding naming an individual outlives their employment; a group membership
    is removed by the same offboarding that removes everything else.
  EOT
  type        = set(string)

  validation {
    condition     = length(var.accessor_groups) > 0
    error_message = "At least one group is required. An empty set produces a binding that admits nobody, which presents as every sign-in failing after a successful apply."
  }

  # THE control, now that a Google-managed OAuth client means anyone can reach the consent
  # screen. `allAuthenticatedUsers` reads like "people who signed in" and means "anyone with a
  # Google account" — and the resulting access is silent, because the sign-in simply succeeds.
  validation {
    condition = alltrue([
      for g in var.accessor_groups :
      g != "allUsers" && g != "allAuthenticatedUsers" &&
      !startswith(g, "allUsers") && !startswith(g, "allAuthenticatedUsers")
    ])
    error_message = "allUsers and allAuthenticatedUsers are forbidden. With a Google-managed OAuth client this binding is the only thing between the internet and a completed sign-in."
  }

  # A BARE address. The module adds the `group:` prefix, so an entry that already carries one
  # becomes `group:group:eng@example.com` — which IAM rejects with an error naming the
  # malformed member and not this file. The colon check is separate from the address shape
  # because `group:eng@example.com` satisfies a naive address regex: `group:eng` contains no
  # `@`, so it reads as a valid local part.
  validation {
    condition = alltrue([
      for g in var.accessor_groups : !strcontains(g, ":")
    ])
    error_message = "Entries must be bare addresses without an IAM type prefix. The module adds group: itself, so \"group:eng@example.com\" would become \"group:group:eng@example.com\"."
  }

  validation {
    condition = alltrue([
      for g in var.accessor_groups : can(regex("^[^@]+@[^@]+\\.[^@]+$", g))
    ])
    error_message = "Each entry must be a group address such as engineering@mindclade.com."
  }

  # A user address here would be accepted by IAM and would create exactly the binding this
  # module exists to prevent. The heuristic is imperfect but catches the common shape.
  validation {
    condition = alltrue([
      for g in var.accessor_groups : !can(regex("^(?i)(admin|root|founder)@", g))
    ])
    error_message = "This looks like an individual or a superuser address rather than a group. Routine access needs a revocation path that offboarding already covers."
  }
}
