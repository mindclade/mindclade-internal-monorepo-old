# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

run "additive_group_access" {
  command = plan

  variables {
    project_id = "mindclade-production"
    backend_services = {
      studio = "k8s2-um-studio-abc123"
    }
    accessor_groups = [
      "engineering@mindclade.com",
      "scientists@mindclade.com",
    ]
  }

  assert {
    condition = (
      length(google_iap_web_backend_service_iam_member.accessor) == 2 &&
      alltrue([
        for grant in google_iap_web_backend_service_iam_member.accessor :
        grant.role == "roles/iap.httpsResourceAccessor" && startswith(grant.member, "group:")
      ])
    )
    error_message = "Every group/backend pair must receive one additive IAP accessor grant."
  }
}
run "reject_public_principal" {
  command = plan

  variables {
    project_id       = "mindclade-production"
    backend_services = { studio = "k8s2-um-studio-abc123" }
    accessor_groups  = ["allAuthenticatedUsers"]
  }

  expect_failures = [var.accessor_groups]
}

run "reject_prefixed_member" {
  command = plan

  variables {
    project_id       = "mindclade-production"
    backend_services = { studio = "k8s2-um-studio-abc123" }
    accessor_groups  = ["group:engineering@mindclade.com"]
  }

  expect_failures = [var.accessor_groups]
}
