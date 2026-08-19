# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "parent" {
  description = "Resource the organization policies attach to, as organizations/<id> or folders/<id>"
  type        = string

  validation {
    condition     = can(regex("^(organizations|folders)/[0-9]{1,25}$", var.parent))
    error_message = "parent must be organizations/<numeric id> or folders/<numeric id>."
  }
}

variable "boolean_policies" {
  description = <<-EOT
    Boolean constraints keyed by constraint name, without the constraints/ prefix. true
    enforces the constraint; false explicitly does not, which is different from omitting it —
    an omitted constraint inherits, a false one overrides an inherited enforcement.
  EOT
  type        = map(bool)
  default     = {}

  validation {
    condition     = alltrue([for k, v in var.boolean_policies : !startswith(k, "constraints/")])
    error_message = "Give the bare constraint name; the constraints/ prefix is added here."
  }
}

variable "list_policies" {
  description = <<-EOT
    List constraints keyed by constraint name. Exactly one of allowed_values or denied_values
    may be non-empty: Google evaluates an allow list as "nothing else", so supplying both is
    a rule whose effect depends on evaluation order rather than on what was written.
  EOT
  type = map(object({
    allowed_values = optional(list(string), [])
    denied_values  = optional(list(string), [])
  }))
  default = {}

  validation {
    condition     = alltrue([for k, v in var.list_policies : !startswith(k, "constraints/")])
    error_message = "Give the bare constraint name; the constraints/ prefix is added here."
  }

  validation {
    condition = alltrue([
      for k, v in var.list_policies :
      !(length(v.allowed_values) > 0 && length(v.denied_values) > 0)
    ])
    error_message = "A list constraint sets allowed_values or denied_values, never both."
  }

  validation {
    condition = alltrue([
      for k, v in var.list_policies :
      length(v.allowed_values) > 0 || length(v.denied_values) > 0
    ])
    error_message = "A list constraint with neither list set enforces nothing; omit it instead."
  }
}

variable "folder_overrides" {
  description = <<-EOT
    Per-folder relaxations, keyed by folders/<id> then by constraint name. Every override
    carries a reason, because a relaxation whose justification lives in a commit message is
    one nobody can review a year later.

    deny_all = false RESETS to the inherited default rather than permitting everything.
    allow_all = true permits everything and inherits nothing, so a later org-level tightening
    silently does not reach the folder — prefer the reset.
  EOT
  type = map(map(object({
    allow_all = optional(bool)
    deny_all  = optional(bool)
    enforce   = optional(bool)
    reason    = string
  })))
  default = {}

  validation {
    condition = alltrue([
      for folder in keys(var.folder_overrides) : can(regex("^folders/[0-9]{1,25}$", folder))
    ])
    error_message = "Each override key must be folders/<numeric id>."
  }

  validation {
    condition = alltrue(flatten([
      for folder, constraints in var.folder_overrides : [
        for name, o in constraints : length(trimspace(o.reason)) >= 20
      ]
    ]))
    error_message = "Every override needs a reason of at least 20 characters explaining why the folder is exempt."
  }

  validation {
    condition = alltrue(flatten([
      for folder, constraints in var.folder_overrides : [
        for name, o in constraints :
        length(compact([
          o.allow_all == null ? "" : "a", o.deny_all == null ? "" : "d", o.enforce == null ? "" : "e",
        ])) == 1
      ]
    ]))
    error_message = "An override sets exactly one of allow_all, deny_all, or enforce."
  }
}

variable "labels" {
  description = <<-EOT
    Accepted for call-site symmetry with the other org-layer modules and deliberately unused:
    google_org_policy_policy carries no labels. Declared rather than dropped so that passing
    it is not a silent no-op discovered later.
  EOT
  type        = map(string)
  default     = {}
}
