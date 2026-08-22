# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

resource "google_monitoring_notification_channel" "this" {
  for_each = var.notification_channels

  project         = var.project_id
  display_name    = each.key
  type            = each.value.type
  labels          = { email_address = each.value.email }
  enabled         = true
  force_delete    = false
  deletion_policy = "PREVENT"
}

resource "google_monitoring_alert_policy" "this" {
  for_each = var.alert_policies

  project               = var.project_id
  display_name          = each.value.display_name
  severity              = each.value.severity
  combiner              = "OR"
  enabled               = true
  deletion_policy       = "PREVENT"
  notification_channels = [for key in var.default_notification_channels : google_monitoring_notification_channel.this[key].name]
  user_labels           = merge(var.labels, { cluster = var.cluster_name, managed-by = "terraform" })

  conditions {
    display_name = each.value.display_name
    condition_threshold {
      filter          = trimspace(each.value.condition.filter)
      comparison      = each.value.condition.comparison
      threshold_value = each.value.condition.threshold_value
      duration        = each.value.condition.duration
      aggregations {
        alignment_period   = "300s"
        per_series_aligner = each.value.condition.aligner
      }
    }
  }

  documentation {
    content   = each.value.documentation
    mime_type = "text/markdown"
  }
}
