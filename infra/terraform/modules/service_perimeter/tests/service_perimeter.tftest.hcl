# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {
  mock_data "google_access_context_manager_access_policy" {
    defaults = { name = "accessPolicies/123456", title = "mindclade-access-policy" }
  }
}
run "identity_only_dry_run" {
  command = plan
  variables {
    org_id      = "123456789012"
    policy_name = "mindclade-access-policy"
    perimeter = {
      name                    = "mindclade_staging", title = "staging perimeter", use_explicit_dry_run_spec = true,
      resources               = ["projects/123456789"], restricted_services = ["storage.googleapis.com"],
      vpc_accessible_services = { enable_restriction = true, allowed_services = ["RESTRICTED-SERVICES"] }
    }
    ingress_policies = [{
      title = "terraform-read", from = { identities = ["serviceAccount:plan@mindclade-common-ci.iam.gserviceaccount.com"], source_access_levels = ["*"] },
      to    = { resources = ["*"], operations = { "storage.googleapis.com" = { methods = ["*Get*"] } } }
    }]
  }
  assert {
    condition     = google_access_context_manager_service_perimeter.this.use_explicit_dry_run_spec
    error_message = "Initial perimeters must be dry-run only."
  }
}
