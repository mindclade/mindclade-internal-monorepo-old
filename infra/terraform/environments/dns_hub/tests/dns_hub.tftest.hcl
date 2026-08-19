# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# RUN WITH:  terraform test -var-file=terraform.tfvars.example
#
# The var-file is required. The last run block deliberately declares no
# `variables`, so it evaluates the COMMITTED example -- the only copy of this
# root's inputs anyone ever reads, since terraform.tfvars is gitignored. Every
# other run block supplies its own variables and ignores the file.
#
# Why `terraform test` and not `terraform console -var-file=...`: console
# evaluates variables LAZILY. A trivial expression exits 0 against inputs that
# violate every validation in variables.tf, and even `local.zones` never reaches
# project_id because no local references it. A test runs a full plan through the
# mock provider, so every variable is evaluated and every validation fires.

mock_provider "google" {}

# The apex of the mail domain carries CAA, MX, and TXT at once. modules/dns
# keys records by an identifier rather than by owner name specifically so this
# is expressible; before that change the three collided on one "@" key and the
# last one written silently won.
run "apex_carries_three_record_types" {
  command = plan

  variables {
    project_id       = "mc-common-dns"
    security_contact = "security@example.test"
    mail_domain      = "com"
    domains = {
      com = { dns_name = "example.com.", wildcard_certificates = false }
    }
    mail = {
      mx          = ["1 smtp.google.com."]
      spf_include = ["_spf.google.com"]
    }
  }

  assert {
    condition = (
      output.zone_records["com"]["caa"].name == "@" &&
      output.zone_records["com"]["mx"].name == "@" &&
      output.zone_records["com"]["spf"].name == "@" &&
      output.zone_records["com"]["caa"].type == "CAA" &&
      output.zone_records["com"]["mx"].type == "MX" &&
      output.zone_records["com"]["spf"].type == "TXT"
    )
    error_message = "The apex must carry CAA, MX, and TXT as three distinct records on one owner name."
  }

  assert {
    condition     = contains(output.zone_records["com"]["mx"].rrdatas, "1 smtp.google.com.")
    error_message = "A configured mail domain must publish its provider's MX, not the null MX."
  }
}

# `issuewild ";"` is the whole point of wildcard_certificates = false: it makes
# "no wildcard for this domain" a rule Let's Encrypt enforces, rather than a
# convention one cert-manager edit can undo.
run "caa_forbids_wildcards_when_disabled" {
  command = plan

  variables {
    project_id       = "mc-common-dns"
    security_contact = "security@example.test"
    # The -var-file names a mail domain that this block's domains map does not
    # contain. Inherited values are real inputs, so this has to be explicit.
    mail_domain = null
    domains = {
      studio = { dns_name = "example.studio.", wildcard_certificates = false }
    }
  }

  assert {
    condition = contains(
      output.zone_records["studio"]["caa"].rrdatas,
      "0 issuewild \";\"",
    )
    error_message = "wildcard_certificates = false must publish CAA '0 issuewild \";\"' to forbid wildcard issuance."
  }

  assert {
    condition = contains(
      output.zone_records["studio"]["caa"].rrdatas,
      "0 iodef \"mailto:security@example.test\"",
    )
    error_message = "CAA must carry an iodef contact so a CA can report a refused issuance."
  }
}

# issuewild is read INSTEAD OF issue for a wildcard request, so it has to be
# stated even when the answer is the same list.
run "caa_permits_wildcards_when_enabled" {
  command = plan

  variables {
    project_id              = "mc-common-dns"
    security_contact        = "security@example.test"
    certificate_authorities = ["letsencrypt.org"]
    mail_domain             = null
    domains = {
      ai = { dns_name = "example.ai.", wildcard_certificates = true }
    }
  }

  assert {
    condition = (
      contains(output.zone_records["ai"]["caa"].rrdatas, "0 issue \"letsencrypt.org\"") &&
      contains(output.zone_records["ai"]["caa"].rrdatas, "0 issuewild \"letsencrypt.org\"")
    )
    error_message = "wildcard_certificates = true must publish both issue and issuewild for each CA."
  }
}

# Absent records read as "unconfigured" to a receiver, not as "sends no mail".
# The null MX and reject-all SPF are what make an unused domain unusable for
# spoofing.
run "domains_without_mail_publish_an_anti_spoof_posture" {
  command = plan

  variables {
    project_id       = "mc-common-dns"
    security_contact = "security@example.test"
    # The -var-file names a mail domain that this block's domains map does not
    # contain. Inherited values are real inputs, so this has to be explicit.
    mail_domain = null
    domains = {
      ai = { dns_name = "example.ai.", wildcard_certificates = true }
    }
  }

  assert {
    condition = (
      output.zone_records["ai"]["null_mx"].rrdatas == tolist(["0 ."]) &&
      output.zone_records["ai"]["spf_reject"].rrdatas == tolist(["v=spf1 -all"]) &&
      output.zone_records["ai"]["dmarc_reject"].name == "_dmarc"
    )
    error_message = "A domain with no mail must publish null MX, reject-all SPF, and p=reject DMARC."
  }
}

# A send-only domain -- transactional mail out, nothing in -- has SPF and DKIM
# but deliberately no MX. Gating "is mail configured" on mx alone silently
# replaced that SPF with the reject-all record, breaking delivery of everything
# the estate sends.
run "send_only_domain_publishes_spf_and_keeps_null_mx" {
  command = plan

  variables {
    project_id       = "mc-common-dns"
    security_contact = "security@example.test"
    mail_domain      = "com"
    domains = {
      com = { dns_name = "example.com.", wildcard_certificates = false }
    }
    mail = {
      spf_include = ["sendgrid.net"]
      dkim        = { s1 = { selector = "s1", value = "v=DKIM1; k=rsa; p=AAAA" } }
    }
  }

  assert {
    condition = (
      output.zone_records["com"]["spf"].rrdatas == tolist(["v=spf1 include:sendgrid.net -all"]) &&
      output.zone_records["com"]["mx"].rrdatas == tolist(["0 ."]) &&
      output.zone_records["com"]["dkim_s1"].name == "s1._domainkey"
    )
    error_message = "A send-only domain must publish its SPF and DKIM while keeping the null MX."
  }
}

# An empty include list must not produce "v=spf1  -all". The doubled space is
# legal per RFC 7208's ABNF and still trips third-party validators, so nothing
# rejects it and someone eventually reports the domain as misconfigured.
run "spf_never_contains_a_doubled_space" {
  command = plan

  variables {
    project_id       = "mc-common-dns"
    security_contact = "security@example.test"
    mail_domain      = "com"
    domains = {
      com = { dns_name = "example.com.", wildcard_certificates = false }
    }
    mail = { mx = ["1 smtp.google.com."] }
  }

  assert {
    condition     = output.zone_records["com"]["spf"].rrdatas == tolist(["v=spf1 -all"])
    error_message = "SPF with no includes must be exactly \"v=spf1 -all\"."
  }
}

# A typo here does not fail loudly -- it silently gives EVERY domain the
# reject-all posture, including the one meant to receive mail. The first symptom
# is bounced mail days later.
run "mail_domain_must_name_a_real_domain_key" {
  command = plan

  variables {
    project_id       = "mc-common-dns"
    security_contact = "security@example.test"
    mail_domain      = "nosuchkey"
    domains = {
      com = { dns_name = "example.com.", wildcard_certificates = false }
    }
  }

  expect_failures = [var.mail_domain]
}

# "smtp.google.com." without a preference is a plausible paste from a provider's
# documentation. It passes a trailing-dot check and then fails at apply with a
# Cloud DNS error that quotes the rrdata without saying what is wrong with it.
run "mx_without_a_preference_is_rejected" {
  command = plan

  variables {
    project_id       = "mc-common-dns"
    security_contact = "security@example.test"
    mail_domain      = "com"
    domains = {
      com = { dns_name = "example.com.", wildcard_certificates = false }
    }
    mail = { mx = ["smtp.google.com."] }
  }

  expect_failures = [var.mail]
}

run "security_contact_must_not_carry_a_mailto_prefix" {
  command = plan

  variables {
    project_id       = "mc-common-dns"
    security_contact = "mailto:security@example.test"
    domains = {
      com = { dns_name = "example.com.", wildcard_certificates = false }
    }
  }

  expect_failures = [var.security_contact]
}

# NO `variables` BLOCK ON PURPOSE. This run evaluates whatever
# -var-file supplies, which is how the committed terraform.tfvars.example gets
# executed rather than merely read. Without it the example can rot silently:
# real tfvars is gitignored, so nothing else ever loads these values.
run "committed_example_evaluates" {
  command = plan

  assert {
    condition     = length(output.zone_records) == 4
    error_message = "terraform.tfvars.example must declare the estate's four registrable domains."
  }

  # Every domain must forbid or permit wildcards deliberately, and must carry a
  # CAA record at all. A zone with no CAA lets any of the ~90 public CAs issue.
  assert {
    condition = alltrue([
      for records in output.zone_records : contains(keys(records), "caa")
    ])
    error_message = "Every zone must publish CAA; without it any public CA may issue for the domain."
  }
}
