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

  signal_window_seconds = {
    for signal_id, signal in var.signal_alerts : signal_id => {
      duration = try(
        tonumber(substr(signal.duration, 0, length(signal.duration) - 1)) *
        lookup({ s = 1, m = 60, h = 3600 }, substr(signal.duration, -1, 1), 0),
        0,
      )
      alignment = try(
        tonumber(substr(signal.alignment_period, 0, length(signal.alignment_period) - 1)) *
        lookup({ s = 1, m = 60, h = 3600 }, substr(signal.alignment_period, -1, 1), 0),
        0,
      )
      minimum_samples_duration = try(
        tonumber(substr(signal.minimum_samples.duration, 0, length(signal.minimum_samples.duration) - 1)) *
        lookup({ s = 1, m = 60, h = 3600 }, substr(signal.minimum_samples.duration, -1, 1), 0),
        0,
      )
      minimum_samples_alignment = try(
        tonumber(substr(signal.minimum_samples.alignment_period, 0, length(signal.minimum_samples.alignment_period) - 1)) *
        lookup({ s = 1, m = 60, h = 3600 }, substr(signal.minimum_samples.alignment_period, -1, 1), 0),
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

  signal_dashboard_tiles = [
    for index, signal_id in sort(keys(var.signal_alerts)) : {
      xPos   = 0
      yPos   = 4 + (length(var.slos) * 12) + (index * 8)
      width  = 48
      height = 8
      widget = {
        title = var.signal_alerts[signal_id].display_name
        xyChart = {
          chartOptions = {
            mode = "COLOR"
          }
          dataSets = [
            {
              legendTemplate = var.signal_alerts[signal_id].display_name
              plotType       = "LINE"
              targetAxis     = "Y1"
              timeSeriesQuery = {
                timeSeriesFilter = {
                  aggregation = {
                    alignmentPeriod    = var.signal_alerts[signal_id].alignment_period
                    perSeriesAligner   = var.signal_alerts[signal_id].per_series_aligner
                    crossSeriesReducer = var.signal_alerts[signal_id].cross_series_reducer
                    groupByFields      = sort(tolist(var.signal_alerts[signal_id].group_by_fields))
                  }
                  filter = var.signal_alerts[signal_id].filter
                }
              }
            }
          ]
          thresholds = [
            {
              color     = "RED"
              direction = var.signal_alerts[signal_id].comparison == "COMPARISON_GT" ? "ABOVE" : "BELOW"
              label     = "alert threshold"
              value     = var.signal_alerts[signal_id].threshold_value
            }
          ]
        }
      }
    }
  ]

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
        local.signal_dashboard_tiles,
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

resource "google_monitoring_alert_policy" "signal" {
  for_each = var.signal_alerts

  project               = var.project_id
  display_name          = "${var.service_display_name}: ${each.value.display_name}"
  combiner              = each.value.minimum_samples == null ? "OR" : "AND"
  enabled               = true
  severity              = each.value.severity
  notification_channels = sort(tolist(var.notification_channels))
  user_labels           = merge(local.resource_labels, { signal = each.key })
  deletion_policy       = "PREVENT"

  conditions {
    display_name = "Signal crosses ${each.value.comparison == "COMPARISON_GT" ? "upper" : "lower"} threshold"

    condition_threshold {
      filter                  = each.value.filter
      comparison              = each.value.comparison
      duration                = each.value.duration
      threshold_value         = each.value.threshold_value
      evaluation_missing_data = each.value.evaluation_missing_data

      aggregations {
        alignment_period     = each.value.alignment_period
        per_series_aligner   = each.value.per_series_aligner
        cross_series_reducer = each.value.cross_series_reducer
        group_by_fields      = sort(tolist(each.value.group_by_fields))
      }

      trigger {
        count = each.value.trigger_count
      }
    }
  }

  dynamic "conditions" {
    for_each = each.value.minimum_samples == null ? [] : [each.value.minimum_samples]

    content {
      display_name = "Minimum sample guard"

      condition_threshold {
        filter                  = conditions.value.filter
        comparison              = "COMPARISON_GT"
        duration                = conditions.value.duration
        threshold_value         = conditions.value.threshold_value
        evaluation_missing_data = "EVALUATION_MISSING_DATA_INACTIVE"

        aggregations {
          alignment_period     = conditions.value.alignment_period
          per_series_aligner   = conditions.value.per_series_aligner
          cross_series_reducer = conditions.value.cross_series_reducer
          group_by_fields      = sort(tolist(conditions.value.group_by_fields))
        }

        trigger {
          count = 1
        }
      }
    }
  }

  alert_strategy {
    auto_close           = "${var.alert_auto_close_seconds}s"
    notification_prompts = ["OPENED", "CLOSED"]
  }

  documentation {
    content = join("\n", [
      "# ${each.value.display_name}",
      "The governed `${each.key}` signal crossed its reviewed threshold. Validate telemetry freshness before mitigation, then inspect recent changes and dependency health.",
      "Runbook: ${var.runbook_url}",
    ])
    mime_type = "text/markdown"
    subject   = "[${upper(var.environment)}] ${var.service_display_name}: ${each.value.display_name}"

    links {
      display_name = "Responder runbook"
      url          = var.runbook_url
    }
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition = (
        local.signal_window_seconds[each.key].duration >= 60 &&
        local.signal_window_seconds[each.key].duration <= 86400 &&
        local.signal_window_seconds[each.key].duration % 60 == 0 &&
        local.signal_window_seconds[each.key].alignment >= 60 &&
        local.signal_window_seconds[each.key].alignment <= 86400 &&
        local.signal_window_seconds[each.key].alignment % 60 == 0
      )
      error_message = "Signal duration and alignment must be whole-minute windows from 60 seconds through 24 hours so missing-data evaluation remains API-valid."
    }

    precondition {
      condition = each.value.minimum_samples == null || (
        local.signal_window_seconds[each.key].minimum_samples_duration >= 60 &&
        local.signal_window_seconds[each.key].minimum_samples_duration <= 86400 &&
        local.signal_window_seconds[each.key].minimum_samples_duration % 60 == 0 &&
        local.signal_window_seconds[each.key].minimum_samples_alignment >= 60 &&
        local.signal_window_seconds[each.key].minimum_samples_alignment <= 86400 &&
        local.signal_window_seconds[each.key].minimum_samples_alignment % 60 == 0 &&
        each.value.minimum_samples.filter != each.value.filter
      )
      error_message = "A minimum-sample guard must use distinct telemetry and whole-minute duration/alignment windows from 60 seconds through 24 hours."
    }
  }
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
