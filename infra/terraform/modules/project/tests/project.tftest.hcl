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
