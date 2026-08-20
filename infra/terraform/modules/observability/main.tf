# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  metrics_scope_name = "locations/global/metricsScopes/${var.metrics_scope_project_id}"
}

resource "google_monitoring_monitored_project" "this" {
  for_each = var.monitored_project_ids

  metrics_scope = local.metrics_scope_name
  name          = each.value

  lifecycle {
    prevent_destroy = true
  }
}

module "service" {
  for_each = var.services
  source   = "../monitoring"

  project_id               = var.metrics_scope_project_id
  environment              = each.value.environment
  owner                    = each.value.owner
  service_id               = each.key
  service_display_name     = each.value.service_display_name
  runbook_url              = each.value.runbook_url
  notification_channels    = each.value.notification_channels
  slos                     = each.value.slos
  labels                   = each.value.labels
  alert_auto_close_seconds = each.value.alert_auto_close_seconds

  depends_on = [google_monitoring_monitored_project.this]
}
