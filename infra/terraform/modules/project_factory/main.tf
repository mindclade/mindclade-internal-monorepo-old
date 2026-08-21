# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  billing_account_id = trimprefix(var.billing_account, "billingAccounts/")
  network_host_keys  = [for key, project in var.projects : key if project.shared_vpc_host]
  metrics_host_keys  = [for key, project in var.projects : key if project.monitoring_scope_host]
  network_host_id    = length(local.network_host_keys) == 1 ? var.projects[local.network_host_keys[0]].project_id : null
  metrics_host_id    = length(local.metrics_host_keys) == 1 ? var.projects[local.metrics_host_keys[0]].project_id : null
}

module "project" {
  for_each = var.projects
  source   = "../project"

  project_id          = each.value.project_id
  project_name        = each.value.name
  folder_id           = var.folder_id
  billing_account_id  = local.billing_account_id
  environment         = lookup(var.labels, "environment", "global")
  owner               = lookup(var.labels, "owner", "platform")
  data_classification = lookup(var.labels, "data-classification", "internal")
  resource_lifecycle  = "persistent"
  labels              = var.labels
  activate_apis = setunion(
    each.value.services,
    each.value.monitoring_scope_host ? var.extra_services : toset([]),
  )
}

resource "google_resource_manager_lien" "project" {
  for_each = { for key, project in var.projects : key => project if project.lien }

  parent       = "projects/${module.project[each.key].project_number}"
  restrictions = ["resourcemanager.projects.delete"]
  origin       = "mindclade-project-factory"
  reason       = "Critical shared project protected by the Mindclade live estate."
}

resource "google_compute_shared_vpc_host_project" "host" {
  for_each = { for key, project in var.projects : key => project if project.shared_vpc_host }
  project  = module.project[each.key].project_id
}

resource "google_compute_shared_vpc_service_project" "service" {
  for_each = { for key, project in var.projects : key => project if project.shared_vpc_service_project }

  host_project    = local.network_host_id
  service_project = module.project[each.key].project_id
  depends_on      = [google_compute_shared_vpc_host_project.host]
}

resource "google_monitoring_monitored_project" "scope_member" {
  for_each = local.metrics_host_id == null ? {} : {
    for key, project in var.projects : key => project
    if project.project_id != local.metrics_host_id
  }

  metrics_scope = "locations/global/metricsScopes/${local.metrics_host_id}"
  name          = module.project[each.key].project_id
}

resource "google_billing_budget" "project_set" {
  count = var.budget_amount == null ? 0 : 1

  billing_account = local.billing_account_id
  display_name    = "${lookup(var.labels, "environment", "shared")} project-set monthly budget"
  deletion_policy = var.deletion_policy

  budget_filter {
    projects               = [for project in module.project : "projects/${project.project_number}"]
    credit_types_treatment = "INCLUDE_ALL_CREDITS"
  }
  amount {
    specified_amount {
      currency_code = "USD"
      units         = tostring(var.budget_amount)
    }
  }
  dynamic "threshold_rules" {
    for_each = toset([0.5, 0.8, 1.0])
    content {
      threshold_percent = threshold_rules.value
      spend_basis       = "CURRENT_SPEND"
    }
  }
  threshold_rules {
    threshold_percent = 1.0
    spend_basis       = "FORECASTED_SPEND"
  }
  lifecycle {
    prevent_destroy = true
  }
}
