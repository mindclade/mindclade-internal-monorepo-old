# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

run "shared_environment_projects" {
  command = plan
  variables {
    billing_account = "AAAAAA-BBBBBB-CCCCCC"
    folder_id       = "folders/123456789"
    budget_amount   = 1000
    labels          = { environment = "staging", owner = "platform" }
    projects = {
      net = {
        project_id      = "mindclade-staging-net"
        name            = "Staging network"
        services        = ["compute.googleapis.com"]
        lien            = true
        shared_vpc_host = true
      }
      ops = {
        project_id            = "mindclade-staging-ops"
        name                  = "Staging operations"
        services              = ["monitoring.googleapis.com"]
        monitoring_scope_host = true
      }
      platform = {
        project_id                 = "mindclade-staging-platform"
        name                       = "Staging platform"
        services                   = ["container.googleapis.com"]
        shared_vpc_service_project = true
      }
    }
  }
  assert {
    condition = (
      google_compute_shared_vpc_host_project.host["net"].project == "mindclade-staging-net" &&
      google_compute_shared_vpc_service_project.service["platform"].host_project == "mindclade-staging-net" &&
      length(google_billing_budget.project_set) == 1
    )
    error_message = "Environment projects must retain host/service ownership and one aggregate budget."
  }
}

run "reject_service_project_without_host" {
  command = plan
  variables {
    billing_account = "AAAAAA-BBBBBB-CCCCCC"
    folder_id       = "folders/123456789"
    projects = {
      platform = {
        project_id                 = "mindclade-staging-platform"
        name                       = "Staging platform"
        services                   = ["container.googleapis.com"]
        shared_vpc_service_project = true
      }
    }
  }
  expect_failures = [var.projects]
}
