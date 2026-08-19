# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Project the Binary Authorization policy and its attestors live in"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid Google Cloud project ID."
  }
}

variable "cluster" {
  description = <<-EOT
    Cluster the per-namespace rules apply to, as <location>.<cluster_name>. Google matches
    this string literally: a zonal cluster written with its region, or a name that does not
    exist, produces a policy that applies to nothing and reports no error.
  EOT
  type        = string
  default     = null

  validation {
    condition     = var.cluster == null || can(regex("^[a-z0-9-]+\\.[a-z0-9-]+$", var.cluster))
    error_message = "cluster must be <location>.<cluster_name>."
  }
}

variable "attestor_key_ring" {
  description = "Fully qualified KMS key ring that holds the attestor signing keys. Asymmetric SIGN keys, which is why they are not declared alongside the symmetric keys in the kms module."
  type        = string

  validation {
    condition     = can(regex("^projects/[^/]+/locations/[^/]+/keyRings/[^/]+$", var.attestor_key_ring))
    error_message = "attestor_key_ring must be projects/<p>/locations/<l>/keyRings/<r>."
  }
}

variable "default_admission_rule" {
  description = <<-EOT
    The rule applied to any pod not matched by a namespace rule. This is the whole control:
    a permissive default makes every namespace exception decorative.
  EOT
  type = object({
    evaluation_mode         = string
    enforcement_mode        = string
    require_attestations_by = optional(list(string), [])
  })

  validation {
    condition = contains(
      ["ALWAYS_ALLOW", "ALWAYS_DENY", "REQUIRE_ATTESTATION"],
      var.default_admission_rule.evaluation_mode
    )
    error_message = "evaluation_mode must be ALWAYS_ALLOW, ALWAYS_DENY, or REQUIRE_ATTESTATION."
  }

  validation {
    condition = contains(
      ["ENFORCED_BLOCK_AND_AUDIT_LOG", "DRYRUN_AUDIT_LOG_ONLY"],
      var.default_admission_rule.enforcement_mode
    )
    error_message = "enforcement_mode must be ENFORCED_BLOCK_AND_AUDIT_LOG or DRYRUN_AUDIT_LOG_ONLY."
  }

  # REQUIRE_ATTESTATION with an empty attestor list admits everything while reading as the
  # strictest possible setting. It is the single most dangerous way to misconfigure this.
  validation {
    condition = (
      var.default_admission_rule.evaluation_mode != "REQUIRE_ATTESTATION" ||
      length(var.default_admission_rule.require_attestations_by) > 0
    )
    error_message = "REQUIRE_ATTESTATION with no attestors admits every image while looking strict; name at least one."
  }
}

variable "cluster_admission_rules" {
  description = <<-EOT
    Per-namespace exceptions, keyed by namespace. Each is a decision somebody made rather
    than a gap somebody left, which is why they are declared rather than defaulted.
  EOT
  type = map(object({
    evaluation_mode         = string
    enforcement_mode        = string
    require_attestations_by = optional(list(string), [])
  }))
  default = {}

  validation {
    condition = alltrue([
      for ns, r in var.cluster_admission_rules :
      contains(["ALWAYS_ALLOW", "ALWAYS_DENY", "REQUIRE_ATTESTATION"], r.evaluation_mode)
    ])
    error_message = "Each evaluation_mode must be ALWAYS_ALLOW, ALWAYS_DENY, or REQUIRE_ATTESTATION."
  }

  validation {
    condition = alltrue([
      for ns, r in var.cluster_admission_rules :
      contains(["ENFORCED_BLOCK_AND_AUDIT_LOG", "DRYRUN_AUDIT_LOG_ONLY"], r.enforcement_mode)
    ])
    error_message = "Each enforcement_mode must be ENFORCED_BLOCK_AND_AUDIT_LOG or DRYRUN_AUDIT_LOG_ONLY."
  }

  validation {
    condition = alltrue([
      for ns, r in var.cluster_admission_rules :
      r.evaluation_mode != "REQUIRE_ATTESTATION" || length(r.require_attestations_by) > 0
    ])
    error_message = "REQUIRE_ATTESTATION with no attestors admits every image while looking strict."
  }
}

variable "global_policy_evaluation_mode" {
  description = <<-EOT
    Whether Google's own system images bypass this policy. Disabling it means the cluster
    cannot start kube-proxy and the node never becomes ready — a failure that presents as a
    networking problem and takes a long time to trace back to here.
  EOT
  type        = string
  default     = "ENABLE"

  validation {
    condition     = contains(["ENABLE", "DISABLE"], var.global_policy_evaluation_mode)
    error_message = "global_policy_evaluation_mode must be ENABLE or DISABLE."
  }
}

variable "exempt_images" {
  description = <<-EOT
    Image path patterns admitted without attestation. A trailing /* matches one path
    component and /** matches any depth — the difference is why a registry exemption that
    looks right can still deny a nested path.
  EOT
  type        = list(string)
  default     = []

  validation {
    condition     = alltrue([for p in var.exempt_images : !startswith(p, "*")])
    error_message = "An exemption starting with * matches every registry, which is the whole policy switched off."
  }
}

variable "attestors" {
  description = <<-EOT
    Attestors to create, keyed by short name. Each gets a Container Analysis note and an
    asymmetric signing key in attestor_key_ring.
  EOT
  type = map(object({
    description       = string
    kms_protection    = optional(string, "HSM")
    kms_key_algorithm = optional(string, "RSA_SIGN_PKCS1_4096_SHA512")
  }))
  default = {}

  validation {
    condition     = alltrue([for k, v in var.attestors : contains(["SOFTWARE", "HSM"], v.kms_protection)])
    error_message = "kms_protection must be SOFTWARE or HSM."
  }

  validation {
    condition     = alltrue([for k, v in var.attestors : startswith(v.kms_key_algorithm, "RSA_SIGN_") || startswith(v.kms_key_algorithm, "EC_SIGN_")])
    error_message = "An attestor key must be an asymmetric SIGN algorithm."
  }
}

variable "attestor_signers" {
  description = <<-EOT
    Who may create an attestation, keyed by attestor name. Separate from who may deploy.

    An empty list is meaningful and is not the same as omitting the entry: it declares that
    only a human granted the role out of band may sign, which is how the biosecurity attestor
    stays un-automatable.
  EOT
  type        = map(list(string))
  default     = {}
}

variable "labels" {
  description = "Labels applied to the KMS signing keys. The policy and attestor resources carry none."
  type        = map(string)
  default     = {}
}
