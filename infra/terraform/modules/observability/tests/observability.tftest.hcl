# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

variables {
  metrics_scope_project_id = "mindclade-observability"
  monitored_project_ids = [
    "mindclade-production",
    "mindclade-security",
  ]
  services = {
    control-plane = {
      environment          = "production"
      owner                = "security-and-operations"
      service_display_name = "Control plane"
      runbook_url          = "https://runbooks.example.com/control-plane"
      notification_channels = [
        "projects/mindclade-observability/notificationChannels/on-call",
      ]
      slos = {
        availability = {
          display_name         = "Request availability"
          goal                 = 0.999
          good_service_filter  = "project = \"mindclade-production\" AND metric.type=\"custom.googleapis.com/http/good_requests\" AND resource.type=\"generic_task\""
          total_service_filter = "project = \"mindclade-production\" AND metric.type=\"custom.googleapis.com/http/requests\" AND resource.type=\"generic_task\""
        }
      }
    }
  }
}

run "metrics_scope_and_service_composition" {
  command = plan

  assert {
    condition = (
      output.metrics_scope_name == "locations/global/metricsScopes/mindclade-observability" &&
      length(google_monitoring_monitored_project.this) == 2 &&
      google_monitoring_monitored_project.this["mindclade-production"].metrics_scope == "locations/global/metricsScopes/mindclade-observability" &&
      google_monitoring_monitored_project.this["mindclade-production"].name == "mindclade-production"
    )
    error_message = "Existing projects must attach to the intended global metrics scope."
  }

  assert {
    condition = (
      module.service["control-plane"].service_id == "control-plane" &&
      output.slo_contracts["control-plane"]["availability"].goal == 0.999 &&
      output.slo_contracts["control-plane"]["availability"].rolling_period_days == 28 &&
      output.runbook_urls["control-plane"] == "https://runbooks.example.com/control-plane"
    )
    error_message = "Service monitoring must be composed through the existing monitoring module without changing its SLO contract."
  }
}

run "multiple_service_compositions" {
  command = plan

  variables {
    services = {
      control-plane = {
        environment          = "production"
        owner                = "security-and-operations"
        service_display_name = "Control plane"
        runbook_url          = "https://runbooks.example.com/control-plane"
        notification_channels = [
          "projects/mindclade-observability/notificationChannels/on-call",
        ]
        slos = {
          availability = {
            display_name         = "Request availability"
            goal                 = 0.999
            good_service_filter  = "project = \"mindclade-production\" AND metric.type=\"custom.googleapis.com/http/good_requests\""
            total_service_filter = "project = \"mindclade-production\" AND metric.type=\"custom.googleapis.com/http/requests\""
          }
        }
      }
      build-platform = {
        environment          = "production"
        owner                = "developer-platform"
        service_display_name = "Build platform"
        runbook_url          = "https://runbooks.example.com/build-platform"
        notification_channels = [
          "projects/mindclade-observability/notificationChannels/build-on-call",
        ]
        slos = {
          success = {
            display_name         = "Build success"
            goal                 = 0.99
            good_service_filter  = "project = \"mindclade-production\" AND metric.type=\"custom.googleapis.com/build/succeeded\""
            total_service_filter = "project = \"mindclade-production\" AND metric.type=\"custom.googleapis.com/build/completed\""
          }
        }
      }
    }
  }

  assert {
    condition     = length(module.service) == 2 && output.slo_contracts["build-platform"]["success"].goal == 0.99
    error_message = "Each declared service must receive an independent monitoring composition."
  }
}

run "reject_scoping_project_as_monitored_member" {
  command = plan

  variables {
    monitored_project_ids = ["mindclade-observability"]
  }

  expect_failures = [var.monitored_project_ids]
}

run "reject_invalid_monitored_project" {
  command = plan

  variables {
    monitored_project_ids = ["INVALID_PROJECT"]
  }

  expect_failures = [var.monitored_project_ids]
}

run "reject_empty_service_composition" {
  command = plan

  variables {
    services = {}
  }

  expect_failures = [var.services]
}

run "reject_unscoped_cross_project_slo_filters" {
  command = plan

  variables {
    services = {
      control-plane = {
        environment          = "production"
        owner                = "security-and-operations"
        service_display_name = "Control plane"
        runbook_url          = "https://runbooks.example.com/control-plane"
        notification_channels = [
          "projects/mindclade-observability/notificationChannels/on-call",
        ]
        slos = {
          availability = {
            display_name         = "Request availability"
            goal                 = 0.999
            good_service_filter  = "metric.type=\"custom.googleapis.com/http/good_requests\""
            total_service_filter = "metric.type=\"custom.googleapis.com/http/requests\""
          }
        }
      }
    }
  }

  expect_failures = [var.services]
}

run "reject_invalid_service_id" {
  command = plan

  variables {
    services = {
      INVALID_SERVICE = {
        environment          = "production"
        owner                = "security-and-operations"
        service_display_name = "Control plane"
        runbook_url          = "https://runbooks.example.com/control-plane"
        notification_channels = [
          "projects/mindclade-observability/notificationChannels/on-call",
        ]
        slos = {
          availability = {
            display_name         = "Request availability"
            goal                 = 0.999
            good_service_filter  = "project = \"mindclade-production\" AND metric.type=\"custom.googleapis.com/http/good_requests\""
            total_service_filter = "project = \"mindclade-production\" AND metric.type=\"custom.googleapis.com/http/requests\""
          }
        }
      }
    }
  }

  expect_failures = [var.services]
}
