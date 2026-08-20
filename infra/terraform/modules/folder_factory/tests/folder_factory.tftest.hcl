# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

variables {
  parent = "organizations/123456789012"
  folders = {
    partners = { display_name = "Partners", deletion_protection = true }
    sandbox  = { display_name = "Sandbox", deletion_protection = false }
  }
}

run "folders_default_to_deletion_protected" {
  command = plan

  variables {
    folders = {
      partners = { display_name = "Partners" }
    }
  }

  assert {
    condition     = google_folder.this["partners"].deletion_protection == true
    error_message = "A folder must be deletion-protected unless it explicitly opts out; the default is the safe one."
  }
}

run "deletion_protection_is_respected_when_waived" {
  command = plan

  assert {
    condition     = google_folder.this["sandbox"].deletion_protection == false
    error_message = "sandbox waives deletion protection deliberately and that must be honoured."
  }
}

run "budgets_filter_by_ancestor_not_by_project" {
  command = plan

  variables {
    billing_account = "01A2B3-C4D5E6-F70819"
    folder_budgets  = { sandbox = 2000 }
  }

  # The reason this assertion exists: a project-list filter goes stale the moment
  # 4-projects creates another project, and the budget silently stops covering it.
  assert {
    condition     = length(google_billing_budget.folder["sandbox"].budget_filter[0].resource_ancestors) == 1
    error_message = "A folder budget must filter by resource ancestor so new projects are covered automatically."
  }

  assert {
    condition     = google_billing_budget.folder["sandbox"].amount[0].specified_amount[0].units == "2000"
    error_message = "Budget amount must be carried through as whole currency units."
  }
}

run "a_forecast_rule_is_always_present" {
  command = plan

  variables {
    billing_account = "01A2B3-C4D5E6-F70819"
    folder_budgets  = { sandbox = 2000 }
  }

  assert {
    condition = anytrue([
      for r in google_billing_budget.folder["sandbox"].threshold_rules : r.spend_basis == "FORECASTED_SPEND"
    ])
    error_message = "Without a forecast rule, the first alert arrives when the money is already spent."
  }
}
