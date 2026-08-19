# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# The estate's public DNS: the registrable domains, delegated from the registrar
# to Cloud DNS so that ACME DNS-01 can be solved by an API call.
#
# WHY THIS IS ITS OWN ROOT, not part of environments/development. A registrable
# domain is not per-environment: mindclade.ai is one delegation shared by every
# environment that ever serves a name under it. Splitting a zone across three
# state files would mean three sets of name servers for one domain, and the
# registrar can only be pointed at one.
#
# WHAT IS DELIBERATELY ABSENT. No A, AAAA, or CNAME records -- the DNS module
# rejects them on public zones by validation, because every application hostname
# in this estate resolves privately and one public address record would undo
# that in a single apply. Internal resolution is private zones, which need a VPC
# and therefore land in a later slice. No `_acme-challenge` records either:
# cert-manager writes and removes those during each challenge, and a
# Terraform-owned record at that name fights the solver.
#
# So a successful apply publishes exactly two things per domain: the delegation
# itself, and the CAA policy naming who may issue for it.

locals {
  # CAA at the apex of every zone.
  #
  # `issue` names the CAs allowed to issue a normal certificate. `issuewild`
  # governs wildcards separately, and a CA consults it INSTEAD OF `issue` for a
  # wildcard request rather than in addition to it -- so it has to be stated
  # even when the answer is the same list.
  caa_records = {
    for key, domain in var.domains : key => {
      name = "@"
      type = "CAA"
      ttl  = var.record_ttl
      rrdatas = concat(
        [for ca in var.certificate_authorities : "0 issue \"${ca}\""],
        domain.wildcard_certificates
        ? [for ca in var.certificate_authorities : "0 issuewild \"${ca}\""]
        # ";" is the CAA idiom for "no CA may issue this". On a domain that
        # serves only its apex, this turns the no-wildcard rule into a control
        # a third party enforces, rather than a convention one cert-manager
        # edit can undo.
        : ["0 issuewild \";\""],
        ["0 iodef \"mailto:${var.security_contact}\""],
      )
    }
  }

  # A domain that receives no mail has to say so out loud. Null MX (RFC 7505)
  # plus a reject-all SPF is what makes it unusable for spoofing: absent
  # records are read by receivers as "unconfigured", not as "sends no mail".
  no_mail_records = {
    null_mx = {
      name    = "@"
      type    = "MX"
      ttl     = var.record_ttl
      rrdatas = ["0 ."]
    }
    spf_reject = {
      name    = "@"
      type    = "TXT"
      ttl     = var.record_ttl
      rrdatas = ["v=spf1 -all"]
    }
    dmarc_reject = {
      name    = "_dmarc"
      type    = "TXT"
      ttl     = var.record_ttl
      rrdatas = ["v=DMARC1; p=reject; sp=reject; adkim=s; aspf=s"]
    }
  }

  # Mail records exist only on the domain that receives mail, and only once a
  # provider has been chosen. Until then that zone falls back to the no-mail
  # posture above, so delegation is never blocked on picking a vendor.
  mail_configured = var.mail_domain != null && length(var.mail.mx) > 0

  mail_records = merge(
    {
      mx = {
        name    = "@"
        type    = "MX"
        ttl     = var.record_ttl
        rrdatas = var.mail.mx
      }
      spf = {
        name = "@"
        type = "TXT"
        ttl  = var.record_ttl
        # Exactly one `all` mechanism, appended last. Two of them void the
        # policy silently: receivers take the first match, and the record still
        # reads as valid to every syntax checker.
        rrdatas = ["v=spf1 ${join(" ", [for include in var.mail.spf_include : "include:${include}"])} -all"]
      }
    },
    var.mail.dmarc_rua == null ? {} : {
      dmarc = {
        name = "_dmarc"
        type = "TXT"
        ttl  = var.record_ttl
        # fo=1 asks for a forensic report on any failure, not just total ones.
        rrdatas = ["v=DMARC1; p=${var.mail.dmarc_policy}; rua=mailto:${var.mail.dmarc_rua}; fo=1"]
      }
    },
    {
      for dkim_key, dkim in var.mail.dkim : "dkim_${dkim_key}" => {
        name    = "${dkim.selector}._domainkey"
        type    = "TXT"
        ttl     = var.record_ttl
        rrdatas = [dkim.value]
      }
    },
  )

  zones = {
    for key, domain in var.domains : key => {
      dns_name    = domain.dns_name
      description = domain.description
      visibility  = "public"
      dnssec      = domain.dnssec
      records = merge(
        { caa = local.caa_records[key] },
        key == var.mail_domain && local.mail_configured ? local.mail_records : local.no_mail_records,
      )
    }
  }
}

module "dns" {
  source = "../../modules/dns"

  project_id = var.project_id
  zones      = local.zones

  # Public zones only in this slice, so there is no network to attach and no
  # inbound resolver to point anywhere. Both arrive with the VPC.
  attached_networks  = []
  inbound_forwarding = { enabled = false }

  enable_logging = var.enable_logging
  owner          = var.owner
  labels         = var.labels
}
