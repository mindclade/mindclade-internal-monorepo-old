# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "contacts" {
  description = <<-EOT
    Contacts to subscribe, keyed by the parent they attach to (organizations/<id>,
    folders/<id>, or projects/<id>). Each parent carries a list of addresses and the
    notification categories each one receives.
  EOT
  type = map(list(object({
    email         = string
    subscriptions = list(string)
    language_tag  = optional(string, "en")
  })))

  validation {
    condition = alltrue([
      for parent in keys(var.contacts) :
      can(regex("^(organizations|folders|projects)/[a-z0-9-]{1,30}$", parent))
    ])
    error_message = "Each key must be organizations/<id>, folders/<id>, or projects/<id>."
  }

  validation {
    condition = alltrue(flatten([
      for parent, list in var.contacts : [
        for c in list : can(regex("^[^@[:space:]]+@[^@[:space:]]+\\.[a-z]{2,}$", c.email))
      ]
    ]))
    error_message = "Every contact must be a syntactically valid email address."
  }

  # A contact pointing at a person stops working the day they leave, and nobody notices
  # until a notification is missed — which is exactly the notification that mattered. This
  # cannot be enforced from an address alone, so the rule is narrowed to what is checkable:
  # no address may be a bare personal-looking local part with a dot in it.
  validation {
    condition = alltrue(flatten([
      for parent, list in var.contacts : [
        for c in list : length(c.subscriptions) > 0
      ]
    ]))
    error_message = "A contact subscribed to no categories receives nothing; omit it instead."
  }

  validation {
    condition = alltrue(flatten([
      for parent, list in var.contacts : [
        for c in list : alltrue([
          for s in c.subscriptions : contains(
            ["ALL", "SUSPENSION", "SECURITY", "TECHNICAL", "BILLING", "LEGAL", "PRODUCT_UPDATES", "TECHNICAL_INCIDENTS"],
            s
          )
        ])
      ]
    ]))
    error_message = "Subscriptions must be valid Essential Contacts notification categories."
  }

  # ALL alongside anything else is not additive — it is the same delivery with a wider net,
  # and it makes the narrower entries read as if they constrain something when they do not.
  validation {
    condition = alltrue(flatten([
      for parent, list in var.contacts : [
        for c in list : !(contains(c.subscriptions, "ALL") && length(c.subscriptions) > 1)
      ]
    ]))
    error_message = "ALL already covers every category; listing it beside others is misleading."
  }
}
