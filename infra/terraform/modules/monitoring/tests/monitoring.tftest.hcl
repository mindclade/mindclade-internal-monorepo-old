mock_provider "google" {}

run "actionable_slo_contract" {
  command = plan

  variables {
    project_id           = "mindclade-observability"
    environment          = "production"
    owner                = "security-and-operations"
    service_id           = "control-plane"
    service_display_name = "Control plane"
    runbook_url          = "https://runbooks.example.com/control-plane"
    notification_channels = [
      "projects/mindclade-observability/notificationChannels/on-call",
    ]
    slos = {
      availability = {
        display_name         = "Request availability"
        goal                 = 0.999
        good_service_filter  = "metric.type=\"custom.googleapis.com/http/good_requests\" AND resource.type=\"generic_task\""
        total_service_filter = "metric.type=\"custom.googleapis.com/http/requests\" AND resource.type=\"generic_task\""
      }
    }
    labels = {
      system     = "mindclade"
      managed-by = "somebody-else"
    }
  }

  assert {
    condition = (
      google_monitoring_custom_service.this.deletion_policy == "PREVENT" &&
      google_monitoring_custom_service.this.user_labels["owner"] == "security-and-operations" &&
      google_monitoring_custom_service.this.user_labels["managed-by"] == "terraform"
    )
    error_message = "The custom service must remain deletion-protected and carry enforced ownership labels."
  }

  assert {
    condition = (
      google_monitoring_slo.this["availability"].goal == 0.999 &&
      google_monitoring_slo.this["availability"].rolling_period_days == 28 &&
      google_monitoring_slo.this["availability"].deletion_policy == "PREVENT"
    )
    error_message = "The availability objective must retain its reviewed goal, window, and deletion guard."
  }

  assert {
    condition = (
      google_monitoring_slo.this["availability"].request_based_sli[0].good_total_ratio[0].good_service_filter !=
      google_monitoring_slo.this["availability"].request_based_sli[0].good_total_ratio[0].total_service_filter
    )
    error_message = "The request-based SLI must retain distinct good and total service filters."
  }

  assert {
    condition = (
      google_monitoring_alert_policy.fast_burn["availability"].combiner == "AND" &&
      google_monitoring_alert_policy.fast_burn["availability"].severity == "CRITICAL" &&
      length(google_monitoring_alert_policy.fast_burn["availability"].conditions) == 2 &&
      google_monitoring_alert_policy.fast_burn["availability"].deletion_policy == "PREVENT"
    )
    error_message = "Fast burn must page only when both windows breach and must retain its critical, deletion-protected contract."
  }

  assert {
    condition = (
      google_monitoring_alert_policy.slow_burn["availability"].combiner == "AND" &&
      google_monitoring_alert_policy.slow_burn["availability"].severity == "WARNING" &&
      length(google_monitoring_alert_policy.slow_burn["availability"].conditions) == 2
    )
    error_message = "Sustained burn must require both windows and remain a warning signal."
  }

  assert {
    condition = (
      google_monitoring_alert_policy.fast_burn["availability"].documentation[0].links[0].url == "https://runbooks.example.com/control-plane" &&
      one(google_monitoring_alert_policy.fast_burn["availability"].notification_channels) == "projects/mindclade-observability/notificationChannels/on-call"
    )
    error_message = "Every fast-burn incident must route to on-call and include the responder runbook."
  }

  assert {
    condition = (
      google_monitoring_dashboard.this.deletion_policy == "PREVENT" &&
      strcontains(google_monitoring_dashboard.this.dashboard_json, "https://runbooks.example.com/control-plane") &&
      strcontains(google_monitoring_dashboard.this.dashboard_json, "select_slo_budget") &&
      strcontains(google_monitoring_dashboard.this.dashboard_json, "select_slo_burn_rate")
    )
    error_message = "The protected dashboard must link the runbook and expose SLO budget and burn-rate views."
  }

  assert {
    condition = (
      jsondecode(google_monitoring_dashboard.this.dashboard_json).mosaicLayout.tiles[1].widget.xyChart.dataSets[0].timeSeriesQuery.timeSeriesFilter.aggregation.alignmentPeriod == "60s" &&
      jsondecode(google_monitoring_dashboard.this.dashboard_json).mosaicLayout.tiles[2].widget.xyChart.dataSets[0].timeSeriesQuery.timeSeriesFilter.aggregation.alignmentPeriod == "60s" &&
      jsondecode(google_monitoring_dashboard.this.dashboard_json).mosaicLayout.tiles[2].widget.xyChart.dataSets[1].timeSeriesQuery.timeSeriesFilter.aggregation.alignmentPeriod == "60s"
    )
    error_message = "Every non-NONE dashboard aligner must declare an API-valid alignment period."
  }
}

run "rejects_unrouted_alerts" {
  command = plan

  variables {
    project_id            = "mindclade-observability"
    environment           = "production"
    owner                 = "security-and-operations"
    service_id            = "control-plane"
    service_display_name  = "Control plane"
    runbook_url           = "https://runbooks.example.com/control-plane"
    notification_channels = []
    slos = {
      availability = {
        display_name         = "Request availability"
        goal                 = 0.999
        good_service_filter  = "metric.type=\"custom.googleapis.com/http/good_requests\" AND resource.type=\"generic_task\""
        total_service_filter = "metric.type=\"custom.googleapis.com/http/requests\" AND resource.type=\"generic_task\""
      }
    }
  }

  expect_failures = [
    var.notification_channels,
  ]
}

run "rejects_non_https_runbook" {
  command = plan

  variables {
    project_id           = "mindclade-observability"
    environment          = "production"
    owner                = "security-and-operations"
    service_id           = "control-plane"
    service_display_name = "Control plane"
    runbook_url          = "http://runbooks.example.com/control-plane"
    notification_channels = [
      "projects/mindclade-observability/notificationChannels/on-call",
    ]
    slos = {
      availability = {
        display_name         = "Request availability"
        goal                 = 0.999
        good_service_filter  = "metric.type=\"custom.googleapis.com/http/good_requests\" AND resource.type=\"generic_task\""
        total_service_filter = "metric.type=\"custom.googleapis.com/http/requests\" AND resource.type=\"generic_task\""
      }
    }
  }

  expect_failures = [
    var.runbook_url,
  ]
}

run "rejects_inverted_burn_windows" {
  command = plan

  variables {
    project_id           = "mindclade-observability"
    environment          = "production"
    owner                = "security-and-operations"
    service_id           = "control-plane"
    service_display_name = "Control plane"
    runbook_url          = "https://runbooks.example.com/control-plane"
    notification_channels = [
      "projects/mindclade-observability/notificationChannels/on-call",
    ]
    slos = {
      availability = {
        display_name         = "Request availability"
        goal                 = 0.999
        good_service_filter  = "metric.type=\"custom.googleapis.com/http/good_requests\" AND resource.type=\"generic_task\""
        total_service_filter = "metric.type=\"custom.googleapis.com/http/requests\" AND resource.type=\"generic_task\""
        fast_burn = {
          threshold      = 6
          short_lookback = "3600s"
          long_lookback  = "300s"
        }
      }
    }
  }

  expect_failures = [google_monitoring_custom_service.this]
}

run "rejects_burn_window_over_24_hours" {
  command = plan

  variables {
    project_id           = "mindclade-observability"
    environment          = "production"
    owner                = "security-and-operations"
    service_id           = "control-plane"
    service_display_name = "Control plane"
    runbook_url          = "https://runbooks.example.com/control-plane"
    notification_channels = [
      "projects/mindclade-observability/notificationChannels/on-call",
    ]
    slos = {
      availability = {
        display_name         = "Request availability"
        goal                 = 0.999
        good_service_filter  = "metric.type=\"custom.googleapis.com/http/good_requests\" AND resource.type=\"generic_task\""
        total_service_filter = "metric.type=\"custom.googleapis.com/http/requests\" AND resource.type=\"generic_task\""
        slow_burn = {
          short_lookback = "12h"
          long_lookback  = "25h"
        }
      }
    }
  }

  expect_failures = [google_monitoring_custom_service.this]
}
