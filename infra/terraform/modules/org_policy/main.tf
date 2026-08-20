# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# Organization Policy v2.
#
# The v2 API, not the v1 `google_organization_policy`. v1 cannot express a folder-level
# override without replacing the whole policy, which is exactly the operation that loses an
# org-level rule nobody remembered was there.
#
# Every policy here is named `<parent>/policies/<constraint>`. That name is the identity, so
# moving a constraint between the boolean and list maps is a destroy-and-create — and the
# window in between is a window with no enforcement.

locals {
  # A constraint declared in both maps would produce two resources with the same name and a
  # duplicate-resource error at apply, long after the plan looked fine.
  overlapping = setintersection(keys(var.boolean_policies), keys(var.list_policies))

  # Folder overrides flattened to one resource per (folder, constraint).
  overrides = merge([
    for folder, constraints in var.folder_overrides : {
      for name, o in constraints : "${folder}:${name}" => {
        folder    = folder
        name      = name
        allow_all = o.allow_all
        deny_all  = o.deny_all
        enforce   = o.enforce
        reason    = o.reason
      }
    }
  ]...)
}

resource "google_org_policy_policy" "boolean" {
  for_each = var.boolean_policies

  name   = "${var.parent}/policies/${each.key}"
  parent = var.parent

  spec {
    # inherit_from_parent is meaningless at the organization level and rejected there, so it
    # is set only when this module is applied to a folder.
    inherit_from_parent = startswith(var.parent, "folders/") ? true : null

    rules {
      enforce = each.value ? "TRUE" : "FALSE"
    }
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = length(local.overlapping) == 0
      error_message = "Declared as both a boolean and a list constraint: ${join(", ", local.overlapping)}."
    }
  }
}

resource "google_org_policy_policy" "list" {
  for_each = var.list_policies

  name   = "${var.parent}/policies/${each.key}"
  parent = var.parent

  spec {
    inherit_from_parent = startswith(var.parent, "folders/") ? true : null

    rules {
      values {
        allowed_values = length(each.value.allowed_values) > 0 ? each.value.allowed_values : null
        denied_values  = length(each.value.denied_values) > 0 ? each.value.denied_values : null
      }
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}

# Folder relaxations.
#
# inherit_from_parent = true is what makes a reset a reset: the folder picks up the org rule
# and this policy then adjusts it. Set to false, the folder would start from nothing and a
# future org-level tightening would never reach it.
resource "google_org_policy_policy" "folder_override" {
  for_each = local.overrides

  name   = "${each.value.folder}/policies/${each.value.name}"
  parent = each.value.folder

  spec {
    inherit_from_parent = true

    rules {
      allow_all = each.value.allow_all == true ? "TRUE" : null
      deny_all  = each.value.deny_all == true ? "TRUE" : null
      enforce   = each.value.enforce == null ? null : (each.value.enforce ? "TRUE" : "FALSE")
    }
  }


  lifecycle {
    prevent_destroy = true
  }
}
