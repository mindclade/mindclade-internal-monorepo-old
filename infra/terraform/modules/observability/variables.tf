# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "metrics_scope_project_id" {
  description = "Existing scoping project that owns the metrics scope and composed Monitoring resources"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.metrics_scope_project_id))
    error_message = "metrics_scope_project_id must be a valid 6-30 character GCP project ID."
  }
}

variable "monitored_project_ids" {
  description = "Additional existing projects attached to the scoping project's metrics scope"
  type        = set(string)
  default     = []

  validation {
    condition = length(var.monitored_project_ids) <= 25 && alltrue([
      for project_id in var.monitored_project_ids :
      can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", project_id))
    ])
    error_message = "monitored_project_ids must contain at most 25 valid GCP project IDs."
  }

  validation {
    condition     = !contains(var.monitored_project_ids, var.metrics_scope_project_id)
    error_message = "Do not add the scoping project to its own metrics scope; it is included automatically."
  }
}

variable "services" {
  description = "Service-monitoring compositions keyed by stable custom-service ID; SLO semantics are implemented by ../monitoring"
  type = map(object({
    environment           = string
    owner                 = string
    service_display_name  = string
    runbook_url           = string
    notification_channels = set(string)
    slos = map(object({
      display_name         = string
      goal                 = number
      rolling_period_days  = optional(number, 28)
      good_service_filter  = string
      total_service_filter = string
      fast_burn = optional(object({
        threshold      = optional(number, 14.4)
        short_lookback = optional(string, "300s")
        long_lookback  = optional(string, "3600s")
      }), {})
      slow_burn = optional(object({
        threshold      = optional(number, 6)
        short_lookback = optional(string, "1800s")
        long_lookback  = optional(string, "21600s")
      }), {})
    }))
    signal_alerts = optional(map(object({
      display_name            = string
      filter                  = string
      comparison              = string
      threshold_value         = number
      duration                = optional(string, "60s")
      severity                = optional(string, "WARNING")
      alignment_period        = optional(string, "60s")
      per_series_aligner      = optional(string, "ALIGN_MAX")
      cross_series_reducer    = optional(string, "REDUCE_MAX")
      group_by_fields         = optional(set(string), [])
      evaluation_missing_data = optional(string, "EVALUATION_MISSING_DATA_ACTIVE")
      trigger_count           = optional(number, 1)
      minimum_samples = optional(object({
        filter               = string
        threshold_value      = number
        duration             = optional(string, "60s")
        alignment_period     = optional(string, "60s")
        per_series_aligner   = optional(string, "ALIGN_MAX")
        cross_series_reducer = optional(string, "REDUCE_SUM")
        group_by_fields      = optional(set(string), [])
      }), null)
    })), {})
    labels                   = optional(map(string), {})
    alert_auto_close_seconds = optional(number, 604800)
  }))

  validation {
    condition = length(var.services) >= 1 && length(var.services) <= 25 && alltrue([
      for service_id in keys(var.services) :
      can(regex("^[a-z][a-z0-9-]{1,61}[a-z0-9]$", service_id))
    ])
    error_message = "services must contain 1-25 entries keyed by valid 3-63 character Monitoring custom-service IDs."
  }

  validation {
    condition = alltrue(flatten([
      for service in values(var.services) : [
        for slo in values(service.slos) :
        can(regex("(^|[[:space:]()])project[[:space:]]*=[[:space:]]*\"[a-z][a-z0-9-]{4,28}[a-z0-9]\"", slo.good_service_filter)) &&
        can(regex("(^|[[:space:]()])project[[:space:]]*=[[:space:]]*\"[a-z][a-z0-9-]{4,28}[a-z0-9]\"", slo.total_service_filter))
      ]
    ]))
    error_message = "Every good and total service filter must explicitly select a project to prevent accidental cross-project SLO aggregation."
  }

  validation {
    condition = alltrue(flatten([
      for service in values(var.services) : [
        for signal in values(service.signal_alerts) :
        length(regexall("(^|[[:space:]()])project[[:space:]]*=[[:space:]]*\"[a-z][a-z0-9-]{4,28}[a-z0-9]\"", signal.filter)) == 1 &&
        (
          signal.minimum_samples == null ||
          length(regexall("(^|[[:space:]()])project[[:space:]]*=[[:space:]]*\"[a-z][a-z0-9-]{4,28}[a-z0-9]\"", signal.minimum_samples.filter)) == 1
        )
      ]
    ]))
    error_message = "Every signal and minimum-sample filter must contain exactly one explicit project selector to prevent accidental cross-project alert aggregation."
  }
}
