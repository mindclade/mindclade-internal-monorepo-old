# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# Nothing in this root is hardcoded to one estate. Every project id, domain, and
# address is an input, so a fork points it at its own registrar and its own
# project without editing a .tf file. terraform.tfvars.example is the filled-in
# form of this interface.

variable "project_id" {
  description = <<-EOT
    Project that will hold every public zone.

    Created elsewhere -- neither this root nor the DNS module creates a project,
    so that losing a delegation cannot be a side effect of a project refactor.
    Bootstrap it once with `gcloud projects create`, then pass the id here.
  EOT
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid GCP project id: 6-30 characters, lowercase letters, digits, and hyphens, starting with a letter."
  }
}

variable "domains" {
  description = <<-EOT
    The estate's registrable domains, keyed by the role each one plays. The key
    becomes the Cloud DNS zone name, so it is a short identifier rather than a
    hostname.

    `wildcard_certificates` controls the CAA posture and nothing else:

      true  -- the plane serves names below the apex, so a CA may issue a
               wildcard for it.
      false -- CAA actively FORBIDS wildcard issuance (`issuewild ";"`). This is
               not the same as declining to request one: it stops any CA from
               issuing a wildcard for the domain even if someone later asks,
               and the refusal is visible in Certificate Transparency.

    `mail` is set only on the domain that receives mail. A domain with no mail
    gets a null-MX and a reject-all SPF, which is what makes it unusable for
    spoofing rather than merely unused.
  EOT

  type = map(object({
    dns_name              = string
    description           = optional(string)
    wildcard_certificates = optional(bool, true)
    dnssec                = optional(bool, true)
  }))

  validation {
    condition     = alltrue([for domain in var.domains : endswith(domain.dns_name, ".")])
    error_message = "Each dns_name must be fully qualified and end with a trailing dot, e.g. \"mindclade.ai.\"."
  }

  validation {
    condition     = length(var.domains) > 0
    error_message = "At least one domain is required; a DNS hub with no zones produces no name servers to delegate to."
  }
}

variable "mail_domain" {
  description = <<-EOT
    Key in `domains` that receives mail, or null when the estate has none.

    This is the domain the ACME account address belongs to. Let's Encrypt sends
    expiry warnings there and the address is awkward to change afterwards, so
    its MX has to be live BEFORE the first certificate is requested -- which is
    why mail records and zone delegation land in the same apply.
  EOT
  type        = string
  default     = null

  # Membership, not shape. A typo here does not fail -- it silently gives EVERY
  # domain the reject-all posture, including the one that is supposed to receive
  # mail, and the first symptom is bounced mail days later. Cross-variable
  # references in validation need Terraform >= 1.9, which versions.tf requires.
  validation {
    condition     = var.mail_domain == null || contains(keys(var.domains), var.mail_domain)
    error_message = "mail_domain must be a key in the domains map (e.g. \"com\"), not a domain name."
  }
}

variable "mail" {
  description = <<-EOT
    Mail records for `mail_domain`. Left empty until a provider is chosen; the
    zone still applies, so delegation is never blocked on picking one.

    `mx` entries are full Cloud DNS rrdata strings, "<preference> <host.>", with
    the trailing dot. Google Workspace is one entry: "1 smtp.google.com.".

    `spf_include` is the provider's include mechanism -- "_spf.google.com" for
    Workspace. The record is assembled here rather than pasted so it cannot
    accumulate two `all` mechanisms, which silently voids the whole policy.
  EOT

  type = object({
    mx          = optional(list(string), [])
    spf_include = optional(list(string), [])

    # Starts at none so a misconfigured selector cannot bounce real mail on day
    # one. Move to quarantine, then reject, once the DMARC reports are clean.
    dmarc_policy = optional(string, "none")
    dmarc_rua    = optional(string)

    # DKIM is emitted verbatim: the value is a provider-generated public key,
    # and there is nothing useful to validate about it here.
    dkim = optional(map(object({
      selector = string
      value    = string
    })), {})
  })

  default = {}

  validation {
    condition     = contains(["none", "quarantine", "reject"], var.mail.dmarc_policy)
    error_message = "dmarc_policy must be \"none\", \"quarantine\", or \"reject\"."
  }

  # Shape, not just the trailing dot. "smtp.google.com." alone is a plausible
  # thing to paste from a provider's docs, passes a dot-only check, and then
  # fails at apply with a Cloud DNS error that quotes the rrdata without saying
  # what is wrong with it.
  validation {
    condition     = alltrue([for record in var.mail.mx : can(regex("^[0-9]+ [^ ]+\\.$", record))])
    error_message = "Each MX rrdata must be \"<preference> <host.>\" -- a number, one space, and a fully qualified host ending in a dot, e.g. \"1 smtp.google.com.\"."
  }
}

variable "certificate_authorities" {
  description = <<-EOT
    CAs permitted to issue for these domains, as CAA `issue` values.

    CAA is the only control here that binds a third party: with it, a CA that is
    not listed must refuse an issuance request even from someone who has proven
    domain control. Without it, any of the ~90 public CAs will issue to whoever
    passes their challenge.

    cert-manager's ACME solver is Let's Encrypt, so the default is exactly that
    and nothing else.
  EOT
  type        = list(string)
  default     = ["letsencrypt.org"]

  validation {
    condition     = length(var.certificate_authorities) > 0
    error_message = "At least one CA is required; an empty issue set forbids all issuance, including cert-manager's."
  }
}

variable "security_contact" {
  description = <<-EOT
    Address published in each zone's CAA `iodef` record, where a CA reports a
    policy violation it refused.

    Distinct from the ACME account address in intent even when the mailbox is
    the same: this one is read by a CA telling you someone tried to get a
    certificate they should not have.
  EOT
  type        = string

  # ":" is excluded, not just whitespace and "@". Without that the promise in
  # the error message is not kept: "mailto:security@example.com" matches a
  # naive address pattern, and main.tf would emit CAA
  # '0 iodef "mailto:mailto:security@example.com"' -- a malformed record that
  # Cloud DNS accepts and no CA can act on.
  validation {
    condition     = can(regex("^[^@:[:space:]]+@[^@:[:space:]]+\\.[^@:[:space:]]+$", var.security_contact))
    error_message = "security_contact must be a bare email address, without the mailto: prefix."
  }
}

variable "enable_logging" {
  description = <<-EOT
    Cloud DNS query logging on public zones.

    Low volume next to VPC flow logs, and the only record that answers "what did
    this actually try to resolve" when a name fails. Off by default because it
    is billable.
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
  description = "Additional labels applied to every zone."
  type        = map(string)
  default     = {}
}

variable "record_ttl" {
  description = <<-EOT
    TTL for every record this root publishes.

    An hour is right for records that change on the order of never: CAA, MX,
    SPF, DMARC. It is deliberately NOT the TTL for anything that fails over --
    no such record lives in this root, because every address record in the
    estate is private. Lower it temporarily before a planned mail migration, so
    the cutover is not gated on caches holding the old MX.
  EOT
  type        = number
  default     = 3600

  validation {
    condition     = var.record_ttl >= 60 && var.record_ttl <= 86400
    error_message = "record_ttl must be between 60 and 86400 seconds."
  }
}

variable "impersonate_service_account" {
  description = <<-EOT
    Service account to impersonate for every API call, or null to use the
    caller's own credentials.

    At this stage the DNS host project is the most privileged thing in the
    estate: it holds the delegation for every domain the company owns, and a
    change here is the one change that can take every hostname dark. Applying
    with a human's application-default credentials makes that reachable from any
    laptop that has run `gcloud auth application-default login`, with the audit
    log naming a person rather than a pipeline.

    Impersonation moves the authority onto a service account that a human holds
    roles/iam.serviceAccountTokenCreator on. That grant is revocable in one
    place, it expires with the token rather than with the laptop, and every
    mutation is attributed to the pipeline identity.

    Left null by default because it cannot be set before the service account
    exists, and this root is the FIRST thing applied in a new estate -- there is
    a bootstrap apply where the only available credential is a human's. Set it
    immediately after.
  EOT
  type        = string
  default     = null

  validation {
    condition = var.impersonate_service_account == null || can(regex(
      "^[a-z0-9-]+@[a-z0-9-]+\\.iam\\.gserviceaccount\\.com$",
      var.impersonate_service_account,
    ))
    error_message = "impersonate_service_account must be a full service account email, e.g. \"terraform-dns@my-project.iam.gserviceaccount.com\"."
  }
}
