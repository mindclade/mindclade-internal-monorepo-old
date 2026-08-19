# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  baseline_labels = {
    data-classification = var.data_classification
    environment         = var.environment
    lifecycle           = var.resource_lifecycle
    managed-by          = "terraform"
    owner               = var.owner
  }

  enabled_services = setunion(
    var.activate_apis,
    var.monthly_budget_usd == null ? toset([]) : toset(["billingbudgets.googleapis.com"]),
  )
}

resource "google_project" "this" {
  name                = var.project_name
  project_id          = var.project_id
  folder_id           = var.folder_id
  billing_account     = var.billing_account_id
  auto_create_network = false
  deletion_policy     = "PREVENT"

  labels = merge(var.labels, local.baseline_labels)

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_project_service" "required" {
  for_each = local.enabled_services

  project            = google_project.this.project_id
  service            = each.value
  disable_on_destroy = false
  deletion_policy    = "PREVENT"
}

resource "google_billing_budget" "this" {
  count = var.monthly_budget_usd == null ? 0 : 1

  billing_account = var.billing_account_id
  display_name    = "${var.project_name} monthly budget"
  deletion_policy = "PREVENT"

  budget_filter {
    projects               = ["projects/${google_project.this.number}"]
    credit_types_treatment = "INCLUDE_ALL_CREDITS"
  }

  amount {
    specified_amount {
      currency_code = "USD"
      units         = tostring(var.monthly_budget_usd)
    }
  }

  threshold_rules {
    threshold_percent = 0.5
    spend_basis       = "CURRENT_SPEND"
  }

  threshold_rules {
    threshold_percent = 0.8
    spend_basis       = "CURRENT_SPEND"
  }

  threshold_rules {
    threshold_percent = 1.0
    spend_basis       = "CURRENT_SPEND"
  }

  threshold_rules {
    threshold_percent = 1.0
    spend_basis       = "FORECASTED_SPEND"
  }

  all_updates_rule {
    monitoring_notification_channels = var.budget_notification_channels
    disable_default_iam_recipients   = false
    enable_project_level_recipients  = var.enable_project_level_budget_recipients
  }

  lifecycle {
    prevent_destroy = true
  }

  depends_on = [google_project_service.required]
}

resource "google_tags_tag_binding" "this" {
  for_each = var.tag_value_names

  parent    = "//cloudresourcemanager.googleapis.com/projects/${google_project.this.number}"
  tag_value = each.value
}

# Shared VPC service-project attachment.
#
# A workload with its own VPC is a workload outside every firewall rule and outside every
# VPC-SC perimeter, and nothing about the project itself says so. Attaching here — rather
# than leaving it to a separate unit — means a project cannot exist in a state where it has
# been created but not yet placed inside the network its policies assume.
#
# Depends on the API being enabled first: compute.googleapis.com is what serves the
# attachment call, and without the explicit ordering Terraform is free to try the attachment
# against a project that has not turned it on yet.
resource "google_compute_shared_vpc_service_project" "this" {
  count = var.shared_vpc_host_project_id == null ? 0 : 1

  host_project    = var.shared_vpc_host_project_id
  service_project = google_project.this.project_id

  depends_on = [google_project_service.required]
}

# The default compute service account holds roles/editor on its own project. The org policy
# in bootstrap already denies the automatic IAM grant; this removes the identity itself, so
# nothing can fall back to it when a Workload Identity binding is missing.
#
# Removing it is what turns "the pod authenticated as something" into a failure rather than a
# silent escalation to project editor.
#
# DEPRIVILEGE, not DELETE. Deleting the account is recoverable for 30 days and permanent
# after that, and a later workload that legitimately needs it — a Dataflow job, a managed
# service that provisions through it — fails with an error naming a missing account rather
# than a missing role. Deprivileging strips roles/editor and is reversible.
resource "google_project_default_service_accounts" "this" {
  count = var.remove_default_service_account ? 1 : 0

  project = google_project.this.project_id
  action  = "DEPRIVILEGE"

  depends_on = [google_project_service.required]
}
