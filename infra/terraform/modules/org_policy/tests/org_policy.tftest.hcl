# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

variables {
  parent = "organizations/123456789012"
}

run "boolean_false_is_an_override_not_an_omission" {
  command = plan

  variables {
    boolean_policies = {
      "compute.requireShieldedVm" = true
      "compute.vmCanIpForward"    = false
    }
  }

  assert {
    condition     = google_org_policy_policy.boolean["compute.vmCanIpForward"].spec[0].rules[0].enforce == "FALSE"
    error_message = "A false boolean must write an explicit FALSE; omitting it would inherit instead."
  }
}

run "org_parent_does_not_set_inherit_from_parent" {
  command = plan

  variables {
    boolean_policies = { "compute.requireShieldedVm" = true }
  }

  # Google rejects inherit_from_parent at the organization level. Setting it would fail at
  # apply with a message that names the field but not the reason.
  assert {
    condition     = google_org_policy_policy.boolean["compute.requireShieldedVm"].spec[0].inherit_from_parent == null
    error_message = "inherit_from_parent must be unset when the parent is an organization."
  }
}

run "a_constraint_in_both_maps_is_rejected" {
  command = plan

  variables {
    boolean_policies = { "compute.vmCanIpForward" = false }
    list_policies = {
      "compute.vmCanIpForward" = { denied_values = ["x"] }
    }
  }

  expect_failures = [google_org_policy_policy.boolean]
}

run "allowed_and_denied_together_is_rejected" {
  command = plan

  variables {
    list_policies = {
      "gcp.restrictTLSVersion" = {
        allowed_values = ["TLS_VERSION_1_2"]
        denied_values  = ["TLS_VERSION_1"]
      }
    }
  }

  expect_failures = [var.list_policies]
}

run "an_override_without_a_real_reason_is_rejected" {
  command = plan

  variables {
    folder_overrides = {
      "folders/000000000000" = {
        "compute.vmExternalIpAccess" = { deny_all = false, reason = "needed" }
      }
    }
  }

  expect_failures = [var.folder_overrides]
}

run "folder_overrides_always_inherit" {
  command = plan

  variables {
    folder_overrides = {
      "folders/000000000000" = {
        "compute.vmExternalIpAccess" = {
          deny_all = false
          reason   = "Experiments need reachable endpoints. Isolated folder, no peering."
        }
      }
    }
  }

  assert {
    condition     = google_org_policy_policy.folder_override["folders/000000000000:compute.vmExternalIpAccess"].spec[0].inherit_from_parent == true
    error_message = "An override that does not inherit silently escapes every future org-level tightening."
  }
}
