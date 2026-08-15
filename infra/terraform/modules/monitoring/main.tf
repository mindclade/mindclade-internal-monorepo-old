# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  baseline_labels = {
    environment = var.environment
    managed-by  = "terraform"
    owner       = var.owner
    service     = var.service_id
  }

  resource_labels = merge(var.labels, local.baseline_labels)
  slo_resource_names = {
    for slo_id in keys(var.slos) :
    slo_id => "projects/${var.project_id}/services/${var.service_id}/serviceLevelObjectives/${slo_id}"
  }

  burn_window_seconds = {
    for slo_id, slo in var.slos : slo_id => {
      fast_short = try(
        tonumber(substr(slo.fast_burn.short_lookback, 0, length(slo.fast_burn.short_lookback) - 1)) *
        lookup({ s = 1, m = 60, h = 3600 }, substr(slo.fast_burn.short_lookback, -1, 1), 0),
        0,
      )
      fast_long = try(
        tonumber(substr(slo.fast_burn.long_lookback, 0, length(slo.fast_burn.long_lookback) - 1)) *
        lookup({ s = 1, m = 60, h = 3600 }, substr(slo.fast_burn.long_lookback, -1, 1), 0),
        0,
      )
      slow_short = try(
        tonumber(substr(slo.slow_burn.short_lookback, 0, length(slo.slow_burn.short_lookback) - 1)) *
        lookup({ s = 1, m = 60, h = 3600 }, substr(slo.slow_burn.short_lookback, -1, 1), 0),
        0,
      )
      slow_long = try(
        tonumber(substr(slo.slow_burn.long_lookback, 0, length(slo.slow_burn.long_lookback) - 1)) *
        lookup({ s = 1, m = 60, h = 3600 }, substr(slo.slow_burn.long_lookback, -1, 1), 0),
        0,
      )
    }
  }

  slo_dashboard_tiles = flatten([
    for index, slo_id in sort(keys(var.slos)) : [
      {
        xPos   = 0
        yPos   = 4 + (index * 12)
        width  = 24
        height = 12
        widget = {
          title = "${var.slos[slo_id].display_name}: remaining error budget"
          xyChart = {
            chartOptions = {
              mode = "COLOR"
            }
            dataSets = [
              {
                legendTemplate = "remaining budget"
                plotType       = "LINE"
                targetAxis     = "Y1"
                timeSeriesQuery = {
                  timeSeriesFilter = {
                    aggregation = {
                      alignmentPeriod  = "60s"
                      perSeriesAligner = "ALIGN_NEXT_OLDER"
                    }
                    filter = "select_slo_budget(\"${local.slo_resource_names[slo_id]}\")"
                  }
                  unitOverride = "1"
                }
              }
            ]
            thresholds = [
              {
                color     = "RED"
                direction = "BELOW"
                label     = "budget exhausted"
                value     = 0
              }
            ]
          }
        }
      },
      {
        xPos   = 24
        yPos   = 4 + (index * 12)
        width  = 24
        height = 12
        widget = {
          title = "${var.slos[slo_id].display_name}: error-budget burn rate"
          xyChart = {
            chartOptions = {
              mode = "COLOR"
            }
            dataSets = [
              {
                legendTemplate = "fast ${var.slos[slo_id].fast_burn.long_lookback} lookback"
                plotType       = "LINE"
                targetAxis     = "Y1"
                timeSeriesQuery = {
                  timeSeriesFilter = {
                    aggregation = {
                      alignmentPeriod  = "60s"
                      perSeriesAligner = "ALIGN_NEXT_OLDER"
                    }
                    filter = "select_slo_burn_rate(\"${local.slo_resource_names[slo_id]}\", \"${var.slos[slo_id].fast_burn.long_lookback}\")"
                  }
                  unitOverride = "1"
                }
              },
              {
                legendTemplate = "slow ${var.slos[slo_id].slow_burn.long_lookback} lookback"
                plotType       = "LINE"
                targetAxis     = "Y1"
                timeSeriesQuery = {
                  timeSeriesFilter = {
                    aggregation = {
                      alignmentPeriod  = "60s"
                      perSeriesAligner = "ALIGN_NEXT_OLDER"
                    }
                    filter = "select_slo_burn_rate(\"${local.slo_resource_names[slo_id]}\", \"${var.slos[slo_id].slow_burn.long_lookback}\")"
                  }
                  unitOverride = "1"
                }
              }
            ]
            thresholds = [
              {
                color     = "RED"
                direction = "ABOVE"
                label     = "fast-burn page"
                value     = var.slos[slo_id].fast_burn.threshold
              },
              {
                color     = "YELLOW"
                direction = "ABOVE"
                label     = "slow-burn warning"
                value     = var.slos[slo_id].slow_burn.threshold
              }
            ]
          }
        }
      }
    ]
  ])

  dashboard_json = jsonencode({
    displayName = "${var.service_display_name} service health (${var.environment})"
    labels      = local.resource_labels
    mosaicLayout = {
      columns = 48
      tiles = concat(
        [
          {
            xPos   = 0
            yPos   = 0
            width  = 48
            height = 4
            widget = {
              text = {
                content = join("\n", [
                  "# ${var.service_display_name} (${var.environment})",
                  "Owner: `${var.owner}`",
                  "[Open responder runbook](${var.runbook_url})",
                  "Alerts use paired short/long error-budget burn windows; validate notification routing with a canary before relying on them.",
                ])
                format = "MARKDOWN"
              }
            }
          }
        ],
        local.slo_dashboard_tiles,
      )
    }
  })
}

resource "google_monitoring_custom_service" "this" {
  project         = var.project_id
  service_id      = var.service_id
  display_name    = var.service_display_name
  user_labels     = local.resource_labels
  deletion_policy = "PREVENT"

  lifecycle {
    prevent_destroy = true

    precondition {
      condition = alltrue([
        for slo_id, windows in local.burn_window_seconds :
        windows.fast_short >= 60 &&
        windows.fast_short < windows.fast_long &&
        windows.fast_long <= 86400 &&
        windows.slow_short >= 60 &&
        windows.slow_short < windows.slow_long &&
        windows.slow_long <= 86400 &&
        windows.fast_short < windows.slow_short &&
        windows.fast_long < windows.slow_long &&
        var.slos[slo_id].fast_burn.threshold > var.slos[slo_id].slow_burn.threshold
      ])
      error_message = "Burn windows must be 60 seconds through 24 hours, short before long, fast windows shorter than their slow counterparts, and the fast-burn threshold higher than the slow-burn threshold."
    }
  }
}

resource "google_monitoring_slo" "this" {
  for_each = var.slos

  project             = var.project_id
  service             = google_monitoring_custom_service.this.service_id
  slo_id              = each.key
  display_name        = each.value.display_name
  goal                = each.value.goal
  rolling_period_days = each.value.rolling_period_days
  user_labels         = merge(local.resource_labels, { objective = each.key })
  deletion_policy     = "PREVENT"

  request_based_sli {
    good_total_ratio {
      good_service_filter  = each.value.good_service_filter
      total_service_filter = each.value.total_service_filter
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_monitoring_alert_policy" "fast_burn" {
  for_each = var.slos

  project               = var.project_id
  display_name          = "${var.service_display_name}: ${each.value.display_name} fast burn"
  combiner              = "AND"
  enabled               = true
  severity              = "CRITICAL"
  notification_channels = sort(tolist(var.notification_channels))
  user_labels           = merge(local.resource_labels, { objective = each.key, signal = "fast-burn" })
  deletion_policy       = "PREVENT"

  conditions {
    display_name = "Short window (${each.value.fast_burn.short_lookback}) exceeds ${each.value.fast_burn.threshold}x"

    condition_threshold {
      filter          = "select_slo_burn_rate(\"${local.slo_resource_names[each.key]}\", \"${each.value.fast_burn.short_lookback}\")"
      comparison      = "COMPARISON_GT"
      duration        = "0s"
      threshold_value = each.value.fast_burn.threshold

      trigger {
        count = 1
      }
    }
  }

  conditions {
    display_name = "Long window (${each.value.fast_burn.long_lookback}) exceeds ${each.value.fast_burn.threshold}x"

    condition_threshold {
      filter          = "select_slo_burn_rate(\"${local.slo_resource_names[each.key]}\", \"${each.value.fast_burn.long_lookback}\")"
      comparison      = "COMPARISON_GT"
      duration        = "0s"
      threshold_value = each.value.fast_burn.threshold

      trigger {
        count = 1
      }
    }
  }

  alert_strategy {
    auto_close           = "${var.alert_auto_close_seconds}s"
    notification_prompts = ["OPENED", "CLOSED"]
  }

  documentation {
    content = join("\n", [
      "# Fast error-budget burn",
      "`${each.value.display_name}` is burning faster than ${each.value.fast_burn.threshold}x across both ${each.value.fast_burn.short_lookback} and ${each.value.fast_burn.long_lookback} lookbacks.",
      "Triage user impact, recent changes, dependency health, and telemetry validity. Follow the runbook for mitigation, escalation, and rollback.",
      "Runbook: ${var.runbook_url}",
    ])
    mime_type = "text/markdown"
    subject   = "[${upper(var.environment)}] ${var.service_display_name}: fast SLO burn"

    links {
      display_name = "Responder runbook"
      url          = var.runbook_url
    }
  }

  lifecycle {
    prevent_destroy = true
  }

  depends_on = [google_monitoring_slo.this]
}

resource "google_monitoring_alert_policy" "slow_burn" {
  for_each = var.slos

  project               = var.project_id
  display_name          = "${var.service_display_name}: ${each.value.display_name} slow burn"
  combiner              = "AND"
  enabled               = true
  severity              = "WARNING"
  notification_channels = sort(tolist(var.notification_channels))
  user_labels           = merge(local.resource_labels, { objective = each.key, signal = "slow-burn" })
  deletion_policy       = "PREVENT"

  conditions {
    display_name = "Short window (${each.value.slow_burn.short_lookback}) exceeds ${each.value.slow_burn.threshold}x"

    condition_threshold {
      filter          = "select_slo_burn_rate(\"${local.slo_resource_names[each.key]}\", \"${each.value.slow_burn.short_lookback}\")"
      comparison      = "COMPARISON_GT"
      duration        = "0s"
      threshold_value = each.value.slow_burn.threshold

      trigger {
        count = 1
      }
    }
  }

  conditions {
    display_name = "Long window (${each.value.slow_burn.long_lookback}) exceeds ${each.value.slow_burn.threshold}x"

    condition_threshold {
      filter          = "select_slo_burn_rate(\"${local.slo_resource_names[each.key]}\", \"${each.value.slow_burn.long_lookback}\")"
      comparison      = "COMPARISON_GT"
      duration        = "0s"
      threshold_value = each.value.slow_burn.threshold

      trigger {
        count = 1
      }
    }
  }

  alert_strategy {
    auto_close           = "${var.alert_auto_close_seconds}s"
    notification_prompts = ["OPENED", "CLOSED"]
  }

  documentation {
    content = join("\n", [
      "# Sustained error-budget burn",
      "`${each.value.display_name}` is burning faster than ${each.value.slow_burn.threshold}x across both ${each.value.slow_burn.short_lookback} and ${each.value.slow_burn.long_lookback} lookbacks.",
      "Confirm the signal, inspect the service dashboard and recent changes, then follow the runbook before the budget is exhausted.",
      "Runbook: ${var.runbook_url}",
    ])
    mime_type = "text/markdown"
    subject   = "[${upper(var.environment)}] ${var.service_display_name}: sustained SLO burn"

    links {
      display_name = "Responder runbook"
      url          = var.runbook_url
    }
  }

  lifecycle {
    prevent_destroy = true
  }

  depends_on = [google_monitoring_slo.this]
}

resource "google_monitoring_dashboard" "this" {
  project         = var.project_id
  dashboard_json  = local.dashboard_json
  deletion_policy = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }

  depends_on = [google_monitoring_slo.this]
}
