# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

run "project_factory_contract" {
  command = plan

  variables {
    project_id          = "mindclade-development"
    project_name        = "Mindclade development"
    folder_id           = "folders/123456789012"
    billing_account_id  = "ABCDEF-012345-6789AB"
    environment         = "development"
    owner               = "cloud-platform"
    data_classification = "confidential"
    activate_apis = [
      "logging.googleapis.com",
      "monitoring.googleapis.com",
    ]
    monthly_budget_usd = 500
    tag_value_names    = ["tagValues/234567890123"]
    labels = {
      system     = "mindclade"
      managed-by = "somebody-else"
    }
  }

  assert {
    condition     = google_project.this.auto_create_network == false
    error_message = "Projects must not create the default network."
  }

  assert {
    condition     = google_project.this.deletion_policy == "PREVENT"
    error_message = "Projects must refuse provider-level deletion."
  }

  assert {
    condition = (
      google_project.this.labels["managed-by"] == "terraform" &&
      google_project.this.labels["owner"] == "cloud-platform" &&
      google_project.this.labels["data-classification"] == "confidential" &&
      google_project.this.labels["system"] == "mindclade"
    )
    error_message = "Baseline labels must be enforced while allowing additional labels."
  }

  assert {
    condition = (
      google_project_service.required["logging.googleapis.com"].disable_on_destroy == false &&
      google_project_service.required["billingbudgets.googleapis.com"].deletion_policy == "PREVENT"
    )
    error_message = "Selected APIs and the budget API must remain enabled."
  }

  assert {
    condition     = google_billing_budget.this[0].amount[0].specified_amount[0].units == "500"
    error_message = "The optional project budget must use the supplied whole-dollar amount."
  }

  assert {
    condition     = google_tags_tag_binding.this["tagValues/234567890123"].tag_value == "tagValues/234567890123"
    error_message = "Requested project tag values must be bound."
  }
}

run "budget_is_optional" {
  command = plan

  variables {
    project_id         = "mindclade-sandbox"
    project_name       = "Mindclade sandbox"
    folder_id          = "folders/123456789012"
    billing_account_id = "ABCDEF-012345-6789AB"
    environment        = "development"
    owner              = "cloud-platform"
  }

  assert {
    condition     = length(google_billing_budget.this) == 0
    error_message = "A budget must not be created when monthly_budget_usd is null."
  }
}

run "shared_vpc_and_default_sa_are_off_by_default" {
  command = plan

  variables {
    project_id         = "mindclade-standalone"
    project_name       = "Mindclade standalone"
    folder_id          = "folders/123456789012"
    billing_account_id = "ABCDEF-012345-6789AB"
    environment        = "development"
    owner              = "cloud-platform"
  }

  # Both default to off, because both are things a caller must ask for. A module that attached
  # every project to a Shared VPC by default would attach the one project that is deliberately
  # outside the workload network — `4-projects/<env>/security` — and nothing about the call site
  # would say so.
  assert {
    condition     = length(google_compute_shared_vpc_service_project.this) == 0
    error_message = "A project must not be attached to a Shared VPC unless a host project is named."
  }

  assert {
    condition     = length(google_project_default_service_accounts.this) == 0
    error_message = "The default service account must be left alone unless removal is requested."
  }
}

run "shared_vpc_and_default_sa_when_requested" {
  command = plan

  variables {
    project_id                     = "mindclade-attached"
    project_name                   = "Mindclade attached"
    folder_id                      = "folders/123456789012"
    billing_account_id             = "ABCDEF-012345-6789AB"
    environment                    = "production"
    owner                          = "cloud-platform"
    shared_vpc_host_project_id     = "mindclade-production-net"
    remove_default_service_account = true
  }

  assert {
    condition = (
      google_compute_shared_vpc_service_project.this[0].host_project == "mindclade-production-net" &&
      google_compute_shared_vpc_service_project.this[0].service_project == "mindclade-attached"
    )
    error_message = "The attachment must name the supplied host project and this project as the service project."
  }

  # DEPRIVILEGE, not DELETE. Deleting the account is permanent after 30 days, and a later
  # workload that legitimately needs it fails with an error naming a missing account rather than
  # a missing role — which is a much harder thing to diagnose than a denied permission.
  assert {
    condition     = google_project_default_service_accounts.this[0].action == "DEPRIVILEGE"
    error_message = "The default service account must be deprivileged rather than deleted."
  }
}

run "rejects_invalid_shared_vpc_host_project_id" {
  command = plan

  variables {
    project_id                 = "mindclade-production"
    project_name               = "Mindclade production"
    folder_id                  = "folders/123456789012"
    billing_account_id         = "ABCDEF-012345-6789AB"
    environment                = "production"
    owner                      = "cloud-platform"
    shared_vpc_host_project_id = "Not-A-Project-Id"
  }

  expect_failures = [var.shared_vpc_host_project_id]
}

run "rejects_restricted_project_id" {
  command = plan

  variables {
    project_id         = "mindclade-google-prod"
    project_name       = "Mindclade production"
    folder_id          = "folders/123456789012"
    billing_account_id = "ABCDEF-012345-6789AB"
    environment        = "production"
    owner              = "cloud-platform"
  }

  expect_failures = [var.project_id]
}

run "rejects_invalid_project_name" {
  command = plan

  variables {
    project_id         = "mindclade-production"
    project_name       = "Mindclade production!"
    folder_id          = "folders/123456789012"
    billing_account_id = "ABCDEF-012345-6789AB"
    environment        = "production"
    owner              = "cloud-platform"
  }

  expect_failures = [var.project_name]
}

run "rejects_invalid_budget_notification_channels" {
  command = plan

  variables {
    project_id         = "mindclade-production"
    project_name       = "Mindclade production"
    folder_id          = "folders/123456789012"
    billing_account_id = "ABCDEF-012345-6789AB"
    environment        = "production"
    owner              = "cloud-platform"
    budget_notification_channels = [
      "notificationChannels/email-on-call",
    ]
  }

  expect_failures = [var.budget_notification_channels]
}
