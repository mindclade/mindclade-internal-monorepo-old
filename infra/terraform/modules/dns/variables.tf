# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Project holding every zone and policy this module creates. Created elsewhere; this module never creates a project."
  type        = string
}

variable "attached_networks" {
  description = <<-EOT
    Default network self-links for private zones, used by any zone that does not set its own
    `networks`.

    Attachment is what makes a private zone visible. A private zone attached to no network is
    not an error — it resolves nowhere, silently. A zone attached to a subset of environments
    is worse: the name works in one VPC and NXDOMAINs in another, which reads as a workload
    bug rather than a DNS one.
  EOT
  type        = list(string)
  default     = []
}

variable "zones" {
  description = <<-EOT
    Managed zones, keyed by a short name that becomes the Cloud DNS zone name.

    `records` is keyed by the owner name RELATIVE to the zone — "api" under
    "mindclade.ai." becomes "api.mindclade.ai.". Use "" or "@" for the apex. Passing an
    already-qualified name is rejected rather than silently producing
    "api.mindclade.ai.mindclade.ai.", which resolves nowhere and looks like a propagation
    delay.

    `dnssec` defaults per visibility: on for public, off for private. DNSSEC on a private zone
    signs against a resolver that was never untrusted, and costs a key that has to be rotated.
  EOT

  type = map(object({
    dns_name = string

    # Falls back to a generated string. Not defaulted to "" — Cloud DNS rejects an empty
    # description, and the error names the field without naming the zone.
    description = optional(string)

    visibility = optional(string, "private")
    dnssec     = optional(bool)

    # Overrides var.attached_networks for this zone only. Ignored on public zones.
    networks = optional(list(string))

    # Keyed by an identifier that DEFAULTS to the relative owner name. Set
    # `name` explicitly when one owner carries more than one type -- an apex
    # holding CAA, MX, and SPF needs three entries that resolve to the same
    # name, and a map cannot hold three "@" keys. Cloud DNS keys record sets by
    # name AND type, so these are three distinct sets rather than a collision.
    records = optional(map(object({
      name    = optional(string)
      type    = string
      ttl     = optional(number, 300)
      rrdatas = list(string)
    })), {})
  }))

  validation {
    condition     = alltrue([for z in var.zones : contains(["public", "private"], z.visibility)])
    error_message = "Each zone's visibility must be \"public\" or \"private\"."
  }

  validation {
    condition     = alltrue([for z in var.zones : endswith(z.dns_name, ".")])
    error_message = "Each dns_name must be fully qualified and end with a trailing dot, e.g. \"mindclade.ai.\"."
  }

  # The relative-name rule, enforced rather than documented. Without it the error surfaces as
  # a name that resolves nowhere, days later, with nothing pointing at this file.
  validation {
    condition = alltrue(flatten([
      for z in var.zones : [
        for key, r in z.records :
        !endswith(r.name != null ? r.name : key, ".") &&
        !endswith(r.name != null ? r.name : key, trimsuffix(z.dns_name, "."))
      ]
    ]))
    error_message = "Record owner names are relative to the zone. Use \"api\", not \"api.mindclade.ai\" and not \"api.mindclade.ai.\"; use \"\" or \"@\" for the apex."
  }

  # A public zone with an A, AAAA, or CNAME record is the failure this estate is built to make
  # impossible: every application hostname resolves privately only, and a public address record
  # would undo that in one apply with no other signal. TXT (ACME) and CAA are the only kinds a
  # public zone here has any business carrying.
  validation {
    condition = alltrue(flatten([
      for z in var.zones : [
        for _, r in z.records :
        contains(["TXT", "CAA", "MX", "NS", "SOA"], upper(r.type))
      ] if z.visibility == "public"
    ]))
    error_message = "Public zones may carry only TXT, CAA, MX, or NS records. An A/AAAA/CNAME record on a public zone would publish an internal hostname."
  }
}

variable "inbound_forwarding" {
  description = <<-EOT
    Inbound server policy: a forwarding target address inside the VPC that an on-prem or VPN
    resolver can be pointed at with a conditional forwarder.

    This is the piece most often missed. Without it every private zone resolves correctly from
    inside the VPC and NXDOMAINs from a laptop on the VPN — and the symptom, "works in the
    cluster, not on my machine", sends people looking at the workload.
  EOT

  type = object({
    enabled  = optional(bool, false)
    name     = optional(string, "inbound-forwarding")
    networks = optional(list(string))
  })

  default = {}
}

variable "enable_logging" {
  description = <<-EOT
    Query logging on public zones.

    Low volume next to flow logs, and the one record that answers "what did this try to
    resolve" when a name fails. Public zones only — private-zone query logging is a property
    of the network's DNS policy, not the zone.
  EOT
  type        = bool
  default     = false
}

variable "owner" {
  description = "Team accountable for these zones. Applied as a label so an unexpected zone has someone to ask."
  type        = string
  default     = "platform"
}

variable "labels" {
  description = "Labels applied to every zone, merged over the module's own baseline."
  type        = map(string)
  default     = {}
}
