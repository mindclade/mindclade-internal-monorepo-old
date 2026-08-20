# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# Cloud DNS zones, records, and the inbound server policy.
#
# This module creates no project and no network. It is handed both, because a DNS module that
# also created its project would put zone changes and project lifecycle in one state file —
# and destroying a zone by refactoring a project is not a mistake worth leaving available.

locals {
  baseline_labels = {
    managed-by = "terraform"
    owner      = var.owner
  }

  # Flatten zone × record into one map so a single resource block covers every record set.
  # The key has to be stable across plans, so it is built from the zone key and the relative
  # record name rather than from anything the provider computes.
  records = merge([
    for zone_key, zone in var.zones : {
      for record_key, record in zone.records :
      "${zone_key}/${record_key}" => {
        zone_key = zone_key

        # The map key is an identifier; the owner name is `name` when set and the key
        # otherwise. They differ only when one owner carries several types -- an apex with
        # CAA, MX, and SPF needs three map entries and cannot have three "@" keys.
        #
        # "" and "@" both mean the apex. Anything else is a label prefixed onto dns_name.
        # variables.tf has already rejected an over-qualified name, so this concatenation
        # cannot produce api.mindclade.ai.mindclade.ai.
        # Not coalesce(): it treats "" as absent, and "" is a legal owner name meaning the
        # apex, so an apex record keyed "" would resolve to the key instead of the override.
        name = (
          (record.name != null ? record.name : record_key) == "" ||
          (record.name != null ? record.name : record_key) == "@"
          ? zone.dns_name
          : "${record.name != null ? record.name : record_key}.${zone.dns_name}"
        )

        type    = upper(record.type)
        ttl     = record.ttl
        rrdatas = record.rrdatas
      }
    }
  ]...)

  inbound_networks = coalesce(var.inbound_forwarding.networks, var.attached_networks)
}

# ---------------------------------------------------------------------------------------
# Zones
# ---------------------------------------------------------------------------------------
resource "google_dns_managed_zone" "this" {
  for_each = var.zones

  project    = var.project_id
  name       = each.key
  dns_name   = each.value.dns_name
  visibility = each.value.visibility
  labels     = merge(local.baseline_labels, var.labels)

  # Cloud DNS rejects an empty description, and the resulting error names the field but not
  # the zone — so the fallback is generated rather than left blank.
  description = coalesce(
    each.value.description,
    "${each.value.visibility} zone for ${each.value.dns_name} — managed by terraform",
  )

  # A zone that can be destroyed by a `terraform apply` that removed one map entry is a
  # delegation you can lose by refactoring. Two independent guards, because they fail at
  # different times: `deletion_policy` is enforced by the provider at apply, `prevent_destroy`
  # below by Terraform at plan. Either alone is defeated by the other's escape hatch.
  #
  # Records are deliberately NOT protected this way — they change every time a hostname is
  # added, and a guard there turns each addition into a state-surgery exercise.
  force_destroy   = false
  deletion_policy = "PREVENT"

  dynamic "dnssec_config" {
    # Public zones sign by default; private zones do not. DNSSEC on a private zone protects a
    # path with no untrusted resolver on it, in exchange for a key someone has to rotate.
    for_each = (
      each.value.visibility == "public" && coalesce(each.value.dnssec, true)
      ? [1] : []
    )
    content {
      state         = "on"
      non_existence = "nsec3"
    }
  }

  dynamic "cloud_logging_config" {
    for_each = each.value.visibility == "public" && var.enable_logging ? [1] : []
    content {
      enable_logging = true
    }
  }

  dynamic "private_visibility_config" {
    for_each = each.value.visibility == "private" ? [1] : []
    content {
      dynamic "networks" {
        for_each = toset(coalesce(each.value.networks, var.attached_networks))
        content {
          network_url = networks.value
        }
      }
    }
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition = (
        each.value.visibility == "public" ||
        length(coalesce(each.value.networks, var.attached_networks)) > 0
      )
      error_message = "Private zone \"${each.key}\" is attached to no network, so it would resolve nowhere and report no error. Set attached_networks, or the zone's own networks."
    }
  }
}

# ---------------------------------------------------------------------------------------
# Records
# ---------------------------------------------------------------------------------------
# Cloud DNS owns the apex SOA and NS itself; declaring either produces an "already exists"
# on create and a fight on every subsequent plan.
#
# cert-manager's _acme-challenge TXT records are deliberately NOT managed here. It writes and
# removes them during each DNS-01 challenge, and a Terraform-owned record at that name either
# fights the solver or blocks issuance outright.
resource "google_dns_record_set" "this" {
  for_each = local.records

  project      = var.project_id
  managed_zone = google_dns_managed_zone.this[each.value.zone_key].name

  name    = each.value.name
  type    = each.value.type
  ttl     = each.value.ttl
  rrdatas = each.value.rrdatas

  lifecycle {
    precondition {
      condition     = !contains(["SOA"], each.value.type)
      error_message = "Cloud DNS manages the apex SOA; remove \"${each.key}\" from records."
    }

    precondition {
      condition = !(
        each.value.type == "NS" &&
        each.value.name == google_dns_managed_zone.this[each.value.zone_key].dns_name
      )
      error_message = "Cloud DNS manages the apex NS set; remove \"${each.key}\" from records. Delegating a CHILD zone with NS is fine — this rejects only the apex."
    }
  }
}

# ---------------------------------------------------------------------------------------
# Inbound server policy
# ---------------------------------------------------------------------------------------
# Allocates a forwarding target address in each attached network. Point the on-prem or VPN
# resolver at it with a conditional forwarder for each private zone's domain.
#
# The addresses are an output rather than an input: Cloud DNS assigns them, and they must be
# read back and configured on the resolver by hand. That step is outside Terraform, which is
# exactly why it gets forgotten.
resource "google_dns_policy" "inbound" {
  count = var.inbound_forwarding.enabled ? 1 : 0

  project                   = var.project_id
  name                      = var.inbound_forwarding.name
  description               = "Inbound forwarding target for VPN and peered resolvers."
  enable_inbound_forwarding = true

  dynamic "networks" {
    for_each = toset(local.inbound_networks)
    content {
      network_url = networks.value
    }
  }

  lifecycle {
    precondition {
      condition     = length(local.inbound_networks) > 0
      error_message = "inbound_forwarding is enabled but no networks were given, so no forwarding target would be allocated and off-VPC resolution would fail with no error here."
    }
  }
}
