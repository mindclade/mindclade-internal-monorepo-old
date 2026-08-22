# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# This module had no suite, which mattered most for the part that is easiest to
# get wrong and hardest to notice: turning a RELATIVE owner name into a fully
# qualified one. Every failure mode here produces a record Cloud DNS accepts and
# no resolver answers usefully, so nothing reports it until a name is queried.

mock_provider "google" {}

run "retired_dns_hub_is_only_a_composition_fixture" {
  command = plan

  module {
    source = "./tests/fixtures/dns_hub"
  }

  assert {
    condition     = output.zone_names["mindclade-ai"] == "mindclade-ai"
    error_message = "The retired dns_hub fixture must still exercise the public-zone composition."
  }
}

# The apex holds CAA, MX, and TXT at once.
#
# The records map is keyed by an identifier rather than by owner name precisely
# so this is expressible -- a map cannot hold three "@" keys. Cloud DNS keys
# record sets by name AND type, so these are three distinct sets. Before the
# `name` override existed the three collided on one key and the last one written
# silently won, which is exactly the shape of a mail outage with no diff to
# blame.
run "one_owner_can_carry_several_record_types" {
  command = plan

  variables {
    project_id = "mc-common-dns"
    zones = {
      com = {
        dns_name   = "example.com."
        visibility = "public"
        records = {
          caa = { name = "@", type = "CAA", rrdatas = ["0 issue \"letsencrypt.org\""] }
          mx  = { name = "@", type = "MX", rrdatas = ["1 smtp.google.com."] }
          spf = { name = "@", type = "TXT", rrdatas = ["v=spf1 -all"] }
        }
      }
    }
  }

  assert {
    condition = (
      google_dns_record_set.this["com/caa"].name == "example.com." &&
      google_dns_record_set.this["com/mx"].name == "example.com." &&
      google_dns_record_set.this["com/spf"].name == "example.com."
    )
    error_message = "An explicit name = \"@\" must resolve to the zone apex regardless of the map key."
  }

  assert {
    condition = (
      google_dns_record_set.this["com/caa"].type == "CAA" &&
      google_dns_record_set.this["com/mx"].type == "MX" &&
      google_dns_record_set.this["com/spf"].type == "TXT"
    )
    error_message = "Three records on one owner name must remain three distinct record sets."
  }
}

# The key is the owner name when `name` is absent. This is the common case and
# the reason the override is opt-in rather than mandatory.
run "the_map_key_is_the_owner_name_when_name_is_omitted" {
  command = plan

  variables {
    project_id = "mc-common-dns"
    zones = {
      ai = {
        dns_name   = "example.ai."
        visibility = "public"
        records = {
          "_acme-challenge" = { type = "TXT", rrdatas = ["token"] }
          "@"               = { type = "CAA", rrdatas = ["0 issue \"letsencrypt.org\""] }
        }
      }
    }
  }

  assert {
    condition     = google_dns_record_set.this["ai/_acme-challenge"].name == "_acme-challenge.example.ai."
    error_message = "A bare key must be prefixed onto the zone's dns_name."
  }

  assert {
    condition     = google_dns_record_set.this["ai/@"].name == "example.ai."
    error_message = "A \"@\" key must resolve to the zone apex."
  }
}

# "" is a legal owner name meaning the apex. It is called out because the
# obvious implementation -- coalesce(record.name, record_key) -- treats "" as
# absent and silently returns the key instead, which for an apex override is
# the one case where the two differ.
run "an_empty_owner_name_is_the_apex_not_a_missing_value" {
  command = plan

  variables {
    project_id = "mc-common-dns"
    zones = {
      dev = {
        dns_name   = "example.dev."
        visibility = "public"
        records = {
          apex = { name = "", type = "TXT", rrdatas = ["v=spf1 -all"] }
        }
      }
    }
  }

  assert {
    condition     = google_dns_record_set.this["dev/apex"].name == "example.dev."
    error_message = "An empty owner name must mean the apex, not fall back to the map key."
  }
}

# Rejected at plan rather than producing api.example.ai.example.ai., which
# resolves nowhere and reads like a propagation delay for as long as anyone is
# willing to wait for one.
run "an_over_qualified_owner_name_is_rejected" {
  command = plan

  variables {
    project_id = "mc-common-dns"
    zones = {
      ai = {
        dns_name   = "example.ai."
        visibility = "public"
        records = {
          api = { name = "api.example.ai", type = "TXT", rrdatas = ["x"] }
        }
      }
    }
  }

  expect_failures = [var.zones]
}

# The estate's central DNS claim is that no application hostname resolves
# publicly. One A record on a public zone would undo it in a single apply, with
# nothing else reporting the change.
run "a_public_zone_may_not_carry_an_address_record" {
  command = plan

  variables {
    project_id = "mc-common-dns"
    zones = {
      ai = {
        dns_name   = "example.ai."
        visibility = "public"
        records = {
          api = { type = "A", rrdatas = ["203.0.113.10"] }
        }
      }
    }
  }

  expect_failures = [var.zones]
}

# A delegation is a thing you lose by refactoring, so the zone carries both a
# provider-side deletion policy and a Terraform-side lifecycle guard.
run "public_zones_are_protected_from_deletion" {
  command = plan

  variables {
    project_id = "mc-common-dns"
    zones = {
      ai = {
        dns_name   = "example.ai."
        visibility = "public"
      }
    }
  }

  assert {
    condition     = google_dns_managed_zone.this["ai"].force_destroy == false
    error_message = "A public zone must not be force-destroyable; losing it drops the delegation for the whole domain."
  }
}
