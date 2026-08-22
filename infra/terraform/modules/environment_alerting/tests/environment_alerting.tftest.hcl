# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}
run "actionable_environment_alert" {
  command = plan
  variables {
    project_id                    = "mindclade-staging-observability"
    metrics_scope_project         = "mindclade-staging-ops"
    cluster_name                  = "mindclade-staging"
    notification_channels         = { platform = { type = "email", email = "platform@mindclade.example" } }
    default_notification_channels = ["platform"]
    alert_policies = {
      gpu-idle = {
        display_name = "GPU idle", severity = "WARNING", documentation = "Investigate the autoscaler."
        condition    = { filter = "metric.type=\"kubernetes.io/node/accelerator/duty_cycle\"", comparison = "COMPARISON_LT", threshold_value = 5, duration = "1800s", aligner = "ALIGN_MEAN" }
      }
    }
  }
  assert {
    condition     = length(google_monitoring_alert_policy.this["gpu-idle"].notification_channels) == 1 && google_monitoring_notification_channel.this["platform"].type == "email"
    error_message = "Every alert must route to a declared channel."
  }
}
