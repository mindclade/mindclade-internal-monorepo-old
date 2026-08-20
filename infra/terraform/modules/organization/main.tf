# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  tag_values = {
    for tag_value in flatten([
      for key_alias, tag_key in var.tag_keys : [
        for value_alias, value in tag_key.values : {
          reference   = "${key_alias}/${value_alias}"
          key_alias   = key_alias
          value_alias = value_alias
          short_name  = value.short_name
          description = value.description
        }
      ]
    ]) : tag_value.reference => tag_value
  }
}

# Tags are governance controls. Both the provider-side deletion policy and the
# Terraform lifecycle guard are intentional: retiring taxonomy requires an
# explicit two-step change and review rather than an incidental destroy.
resource "google_tags_tag_key" "this" {
  for_each = var.tag_keys

  parent          = "organizations/${var.organization_id}"
  short_name      = each.value.short_name
  description     = each.value.description
  deletion_policy = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_tags_tag_value" "this" {
  for_each = local.tag_values

  parent          = google_tags_tag_key.this[each.value.key_alias].id
  short_name      = each.value.short_name
  description     = each.value.description
  deletion_policy = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_tags_tag_binding" "this" {
  for_each = var.tag_bindings

  parent          = each.value.parent
  tag_value       = try(google_tags_tag_value.this[each.value.tag_value].id, "tagValues/invalid-reference")
  deletion_policy = "PREVENT"

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = contains(keys(local.tag_values), each.value.tag_value)
      error_message = "tag_bindings[${each.key}].tag_value must reference a value declared in tag_keys using <key-alias>/<value-alias>."
    }
  }
}

# Member resources are deliberately additive. This module never manages an
# organization IAM policy or role-authoritative binding.
resource "google_organization_iam_member" "this" {
  for_each = var.iam_grants

  org_id = var.organization_id
  role   = each.value.role
  member = each.value.member

  dynamic "condition" {
    for_each = each.value.condition == null ? [] : [each.value.condition]

    content {
      title       = condition.value.title
      description = condition.value.description
      expression  = condition.value.expression
    }
  }
}
