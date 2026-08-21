# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Google Cloud metrics-scope project that owns the service, SLOs, alerts, and dashboard"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid 6-30 character Google Cloud project ID."
  }
}

variable "environment" {
  description = "Deployment environment used in labels and notifications"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.environment))
    error_message = "environment must be a valid non-empty Google Cloud label value."
  }
}

variable "owner" {
  description = "Team accountable for responding to this service's alerts"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.owner))
    error_message = "owner must be a valid non-empty Google Cloud label value."
  }
}

variable "service_id" {
  description = "Stable Cloud Monitoring custom-service ID"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,61}[a-z0-9]$", var.service_id))
    error_message = "service_id must contain 3-63 lowercase letters, digits, or hyphens."
  }
}

variable "service_display_name" {
  description = "Human-readable service name used in Monitoring and notifications"
  type        = string

  validation {
    condition     = length(trimspace(var.service_display_name)) >= 3 && length(var.service_display_name) <= 63
    error_message = "service_display_name must contain 3-63 characters."
  }
}

variable "runbook_url" {
  description = "HTTPS responder runbook linked from every alert and the service dashboard"
  type        = string

  validation {
    condition     = can(regex("^https://[^[:space:]]+$", var.runbook_url)) && length(var.runbook_url) <= 2083
    error_message = "runbook_url must be a complete HTTPS URL no longer than 2083 characters."
  }
}

variable "notification_channels" {
  description = "Existing Monitoring notification-channel resource names used by every SLO burn alert"
  type        = set(string)

  validation {
    condition = length(var.notification_channels) >= 1 && length(var.notification_channels) <= 16 && alltrue([
      for channel in var.notification_channels :
      can(regex("^projects/[A-Za-z0-9-]+/notificationChannels/[A-Za-z0-9_-]+$", channel))
    ])
    error_message = "notification_channels must contain 1-16 complete Monitoring notification-channel resource names."
  }
}

variable "slos" {
  description = "Request-based SLO contracts and paired fast/slow error-budget burn windows"
  type = map(object({
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

  validation {
    condition = length(var.slos) >= 1 && length(var.slos) <= 10 && alltrue([
      for slo_id, slo in var.slos :
      can(regex("^[a-z][a-z0-9-]{1,61}[a-z0-9]$", slo_id)) &&
      length(trimspace(slo.display_name)) >= 3 && length(slo.display_name) <= 63
    ])
    error_message = "slos must contain 1-10 objectives with stable 3-63 character IDs and display names."
  }

  validation {
    condition = alltrue([
      for slo in values(var.slos) :
      slo.goal > 0 && slo.goal <= 0.9999 &&
      floor(slo.rolling_period_days) == slo.rolling_period_days &&
      slo.rolling_period_days >= 1 && slo.rolling_period_days <= 30
    ])
    error_message = "Each SLO goal must be greater than 0 and no more than 0.9999, with a whole rolling period from 1 through 30 days."
  }

  validation {
    condition = alltrue([
      for slo in values(var.slos) :
      length(trimspace(slo.good_service_filter)) >= 1 &&
      length(slo.good_service_filter) <= 2048 &&
      strcontains(slo.good_service_filter, "metric.type") &&
      length(trimspace(slo.total_service_filter)) >= 1 &&
      length(slo.total_service_filter) <= 2048 &&
      strcontains(slo.total_service_filter, "metric.type") &&
      slo.good_service_filter != slo.total_service_filter
    ])
    error_message = "Each SLO requires distinct, non-empty Monitoring good/total filters containing metric.type and no more than 2048 characters."
  }

  validation {
    condition = alltrue([
      for slo in values(var.slos) :
      slo.fast_burn.threshold > 1 && slo.fast_burn.threshold <= 1000 &&
      slo.slow_burn.threshold > 1 && slo.slow_burn.threshold <= 1000 &&
      can(regex("^[1-9][0-9]*(s|m|h)$", slo.fast_burn.short_lookback)) &&
      can(regex("^[1-9][0-9]*(s|m|h)$", slo.fast_burn.long_lookback)) &&
      can(regex("^[1-9][0-9]*(s|m|h)$", slo.slow_burn.short_lookback)) &&
      can(regex("^[1-9][0-9]*(s|m|h)$", slo.slow_burn.long_lookback)) &&
      slo.fast_burn.short_lookback != slo.fast_burn.long_lookback &&
      slo.slow_burn.short_lookback != slo.slow_burn.long_lookback
    ])
    error_message = "Burn-rate thresholds must be greater than 1 and no more than 1000; each short/long lookback must be a distinct positive s, m, or h duration. Resource preconditions additionally enforce duration and ordering semantics."
  }
}

variable "signal_alerts" {
  description = "Bounded metric-threshold alerts for correctness, freshness, saturation, and dependency signals"
  type = map(object({
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
  }))
  default  = {}
  nullable = false

  validation {
    condition = length(var.signal_alerts) <= 25 && alltrue([
      for signal_id, signal in var.signal_alerts :
      can(regex("^[a-z][a-z0-9-]{1,61}[a-z0-9]$", signal_id)) &&
      length(trimspace(signal.display_name)) >= 3 && length(signal.display_name) <= 128
    ])
    error_message = "signal_alerts may contain at most 25 alerts with stable 3-63 character IDs and 3-128 character display names."
  }

  validation {
    condition = alltrue([
      for signal in values(var.signal_alerts) :
      length(trimspace(signal.filter)) >= 1 && length(signal.filter) <= 2048 &&
      strcontains(signal.filter, "metric.type") && strcontains(signal.filter, "resource.type") &&
      contains(["COMPARISON_GT", "COMPARISON_LT"], signal.comparison) &&
      signal.threshold_value >= -1000000000000000 && signal.threshold_value <= 1000000000000000 &&
      contains(["CRITICAL", "ERROR", "WARNING"], signal.severity) &&
      contains([
        "EVALUATION_MISSING_DATA_ACTIVE",
        "EVALUATION_MISSING_DATA_INACTIVE",
        "EVALUATION_MISSING_DATA_NO_OP",
      ], signal.evaluation_missing_data)
    ])
    error_message = "Each signal alert requires a bounded metric/resource filter, an API-supported GT/LT comparison, a governed severity, and explicit missing-data behavior."
  }

  validation {
    condition = alltrue([
      for signal in values(var.signal_alerts) :
      can(regex("^[1-9][0-9]*(s|m|h)$", signal.duration)) &&
      can(regex("^[1-9][0-9]*(s|m|h)$", signal.alignment_period)) &&
      contains(["ALIGN_MIN", "ALIGN_MAX", "ALIGN_MEAN", "ALIGN_SUM", "ALIGN_RATE", "ALIGN_PERCENTILE_99", "ALIGN_NEXT_OLDER"], signal.per_series_aligner) &&
      contains(["REDUCE_MIN", "REDUCE_MAX", "REDUCE_MEAN", "REDUCE_SUM", "REDUCE_PERCENTILE_99"], signal.cross_series_reducer) &&
      floor(signal.trigger_count) == signal.trigger_count && signal.trigger_count >= 1 && signal.trigger_count <= 1000 &&
      length(signal.group_by_fields) <= 8 && alltrue([
        for field in signal.group_by_fields :
        can(regex("^(resource\\.type|resource\\.label\\.[A-Za-z0-9_]+|metric\\.label\\.[A-Za-z0-9_]+)$", field))
      ])
    ])
    error_message = "Signal windows must be positive s/m/h durations; aligners, reducers, trigger counts, and up to eight grouping fields must use the bounded Monitoring contract."
  }

  validation {
    condition = alltrue([
      for signal in values(var.signal_alerts) : signal.minimum_samples == null || (
        length(trimspace(signal.minimum_samples.filter)) >= 1 &&
        length(signal.minimum_samples.filter) <= 2048 &&
        strcontains(signal.minimum_samples.filter, "metric.type") &&
        strcontains(signal.minimum_samples.filter, "resource.type") &&
        signal.minimum_samples.threshold_value > 0 &&
        can(regex("^[1-9][0-9]*(s|m|h)$", signal.minimum_samples.duration)) &&
        can(regex("^[1-9][0-9]*(s|m|h)$", signal.minimum_samples.alignment_period)) &&
        contains(["ALIGN_MIN", "ALIGN_MAX", "ALIGN_MEAN", "ALIGN_SUM", "ALIGN_RATE", "ALIGN_NEXT_OLDER"], signal.minimum_samples.per_series_aligner) &&
        contains(["REDUCE_MIN", "REDUCE_MAX", "REDUCE_MEAN", "REDUCE_SUM"], signal.minimum_samples.cross_series_reducer) &&
        signal.minimum_samples.threshold_value <= 1000000000000000 &&
        length(signal.minimum_samples.group_by_fields) <= 8 && alltrue([
          for field in signal.minimum_samples.group_by_fields :
          can(regex("^(resource\\.type|resource\\.label\\.[A-Za-z0-9_]+|metric\\.label\\.[A-Za-z0-9_]+)$", field))
        ])
      )
    ])
    error_message = "A minimum-sample guard requires a bounded metric/resource filter, positive threshold, valid durations, and bounded aggregation."
  }
}

variable "labels" {
  description = "Additional service labels; baseline governance and signal labels take precedence"
  type        = map(string)
  default     = {}

  validation {
    condition = length(var.labels) <= 58 && alltrue([
      for key, value in var.labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
      can(regex("^$|^[a-z0-9][a-z0-9_-]{0,62}$", value))
    ])
    error_message = "labels must leave room for baseline labels and use valid Monitoring label keys and values."
  }
}

variable "alert_auto_close_seconds" {
  description = "Time without data before Monitoring closes a stale incident"
  type        = number
  default     = 604800

  validation {
    condition     = floor(var.alert_auto_close_seconds) == var.alert_auto_close_seconds && var.alert_auto_close_seconds >= 1800 && var.alert_auto_close_seconds <= 604800
    error_message = "alert_auto_close_seconds must be a whole number from 1800 through 604800."
  }
}
