# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# Folders, and the budgets that make an unowned one visible.
#
# A folder is where org policy and IAM are inherited from, so creating one is a governance
# act. The map key is the identity here rather than the display name: Terraform destroys and
# recreates a folder whose key changes, and a recreated folder arrives empty of every policy
# and binding that was attached to the old one.

locals {
  # Budgets are keyed by folder short name, so a budget naming a folder that does not exist
  # is a typo that would otherwise create a budget filtered to nothing and alert on nothing.
  unknown_budget_keys = setsubtract(keys(var.folder_budgets), keys(var.folders))
}

resource "google_folder" "this" {
  for_each = var.folders

  parent              = var.parent
  display_name        = each.value.display_name
  deletion_protection = each.value.deletion_protection

  lifecycle {
    precondition {
      condition     = length(local.unknown_budget_keys) == 0
      error_message = "folder_budgets names folders that do not exist: ${join(", ", local.unknown_budget_keys)}."
    }
  }
}

# A budget filtered by resource_ancestors, not by projects. A project list goes stale the
# moment 4-projects creates another one, and a budget that silently stops covering half a
# folder reads exactly like a folder that got cheaper.
resource "google_billing_budget" "folder" {
  for_each = var.folder_budgets

  billing_account = var.billing_account
  display_name    = "folder-${each.key}"

  budget_filter {
    resource_ancestors = [google_folder.this[each.key].id]
    calendar_period    = "MONTH"
  }

  amount {
    specified_amount {
      currency_code = var.budget_currency
      units         = tostring(each.value)
    }
  }

  dynamic "threshold_rules" {
    for_each = var.budget_threshold_percents

    content {
      threshold_percent = threshold_rules.value
      spend_basis       = "CURRENT_SPEND"
    }
  }

  # A forecast rule alongside the actual-spend rules above. Hitting 100% on the last day of
  # the month is information nobody can act on; a forecast that says so on day nine is.
  threshold_rules {
    threshold_percent = 1.0
    spend_basis       = "FORECASTED_SPEND"
  }

  dynamic "all_updates_rule" {
    for_each = length(var.budget_monitoring_channels) > 0 ? [1] : []

    content {
      monitoring_notification_channels = var.budget_monitoring_channels
      disable_default_iam_recipients   = false
    }
  }

  lifecycle {
    precondition {
      condition     = var.billing_account != ""
      error_message = "folder_budgets is set but billing_account is empty; Google reports this as a permission error rather than a missing field."
    }
  }
}
