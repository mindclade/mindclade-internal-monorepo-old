# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  rules = merge([
    for firewall_key, firewall in var.firewalls : {
      for rule_name, rule in firewall.rules : "${firewall_key}/${rule_name}" => merge(rule, {
        firewall_key = firewall_key
        rule_name    = rule_name
        project_id   = firewall.project_id
        network      = firewall.network
        log_config = rule.log_config != null ? rule.log_config : (
          firewall.enable_logging_on_deny_only && rule.action == "deny"
          ? { metadata = "INCLUDE_ALL_METADATA" }
          : null
        )
      })
    }
  ]...)
}

resource "google_compute_firewall" "rule" {
  for_each = local.rules

  project     = each.value.project_id
  network     = each.value.network
  name        = each.value.rule_name
  description = each.value.description
  direction   = each.value.direction
  priority    = each.value.priority
  disabled    = each.value.disabled

  source_ranges      = length(each.value.source_ranges) > 0 ? each.value.source_ranges : null
  destination_ranges = length(each.value.destination_ranges) > 0 ? each.value.destination_ranges : null
  source_tags        = length(each.value.source_tags) > 0 ? each.value.source_tags : null
  target_tags        = length(each.value.target_tags) > 0 ? each.value.target_tags : null

  dynamic "allow" {
    for_each = each.value.allow
    content {
      protocol = allow.value.protocol
      ports    = length(allow.value.ports) > 0 ? allow.value.ports : null
    }
  }

  dynamic "deny" {
    for_each = each.value.deny
    content {
      protocol = deny.value.protocol
      ports    = length(deny.value.ports) > 0 ? deny.value.ports : null
    }
  }

  dynamic "log_config" {
    for_each = each.value.log_config == null ? [] : [each.value.log_config]
    content {
      metadata = log_config.value.metadata
    }
  }
}
