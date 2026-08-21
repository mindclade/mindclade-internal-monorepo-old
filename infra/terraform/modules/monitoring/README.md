# Cloud Monitoring service contract

This module creates one custom service, one or more request-based SLOs, paired
multi-window error-budget burn alerts, bounded metric-threshold signal alerts,
and a service dashboard. Every alert has an owner, severity, HTTPS runbook
link, open/close notifications, and at least one pre-existing notification channel.
Monitoring resources have both provider- and Terraform-level deletion guards.
Dashboard SLO queries use an explicit 60-second alignment period so their
non-`ALIGN_NONE` aligners satisfy the Cloud Monitoring API contract.
Burn-rate lookbacks are normalized to seconds and must remain between one
minute and 24 hours, with short windows before long windows and fast signals
ordered ahead of their slow counterparts.

Signal alerts are an additive interface for correctness, freshness, saturation,
and dependency metrics that do not fit request-based SLO math. Their filters use
bounded, `AND`-only string-equality conjunctions with one exact metric and
monitored-resource type selector;
duration and alignment are whole-minute windows between one minute and 24 hours.
Complex disjunctions must be reduced into a recording rule first. Missing-data behavior is
always explicit. A signal can add a distinct minimum-sample condition, in which
case the policy uses `AND`: the symptom must breach while enough traffic is
present. The sample condition treats missing data as inactive, while standalone
freshness/target alerts can fail closed with active missing-data evaluation.
Cloud Monitoring evaluates this `AND` at policy scope; it does not join the two
conditions by resource identity. Both filters must therefore reduce to the
intended bounded series. Use a recording rule when per-resource correlation is
required, and prove the fire/resolve behavior against connected telemetry before
activation.

Callers supply the good and total time-series filters because meaningful SLIs
depend on workload telemetry. Both metrics must be `DELTA` or `CUMULATIVE`, have
numeric values, use compatible labels, and satisfy `good <= total`. Validate the
filters with live time-series queries before applying this module; an empty or
misclassified metric produces a misleading SLO even when Terraform succeeds.

```hcl
module "control_plane_monitoring" {
  source = "../../modules/monitoring"

  project_id           = "mindclade-observability"
  environment          = "production"
  owner                = "security-and-operations"
  service_id           = "control-plane"
  service_display_name = "Control plane"
  runbook_url           = "https://runbooks.example.com/control-plane"
  notification_channels = [
    "projects/mindclade-observability/notificationChannels/on-call",
  ]

  slos = {
    availability = {
      display_name = "Request availability"
      goal         = 0.9995
      good_service_filter  = "metric.type=\"custom.googleapis.com/http/good_requests\" AND resource.type=\"generic_task\""
      total_service_filter = "metric.type=\"custom.googleapis.com/http/requests\" AND resource.type=\"generic_task\""
    }
  }

  signal_alerts = {
    admission-latency = {
      display_name            = "Admission p99 latency"
      filter                  = "metric.type=\"prometheus.googleapis.com/mindclade_control_admission_decision_duration_seconds/histogram\" AND resource.type=\"prometheus_target\""
      comparison              = "COMPARISON_GT"
      threshold_value         = 0.1
      severity                = "CRITICAL"
      per_series_aligner      = "ALIGN_PERCENTILE_99"
      evaluation_missing_data = "EVALUATION_MISSING_DATA_ACTIVE"
      minimum_samples = {
        filter               = "metric.type=\"prometheus.googleapis.com/mindclade_control_admission_decisions_total/counter\" AND resource.type=\"prometheus_target\""
        threshold_value      = 9
        per_series_aligner   = "ALIGN_RATE"
        cross_series_reducer = "REDUCE_SUM"
      }
    }
  }
}
```

Enable `monitoring.googleapis.com` before use and manage notification channels in
a separate sensitive lifecycle. After a reviewed plan, validate SLO math against
known traffic, fire and recover a synthetic alert through every route, inspect
dashboard data, test deduplication/escalation, and confirm missing telemetry is
detected independently. Before enabling a signal alert, validate its filter with
a live time-series query and prove intentional fire, notification delivery,
recovery, and absence behavior. The default 14.4x/5m+1h and 6x/30m+6h burn windows are
starting contracts, not substitutes for approved business objectives. This
module is configuration, not evidence that telemetry or response is operational.

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.9.0, < 2.0.0 |
| <a name="requirement_google"></a> [google](#requirement\_google) | >= 7.41.0, < 8.0.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_google"></a> [google](#provider\_google) | >= 7.41.0, < 8.0.0 |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_alert_auto_close_seconds"></a> [alert\_auto\_close\_seconds](#input\_alert\_auto\_close\_seconds) | Time without data before Monitoring closes a stale incident | `number` | `604800` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Deployment environment used in labels and notifications | `string` | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | Additional service labels; baseline governance and signal labels take precedence | `map(string)` | `{}` | no |
| <a name="input_notification_channels"></a> [notification\_channels](#input\_notification\_channels) | Existing Monitoring notification-channel resource names used by every SLO burn alert | `set(string)` | n/a | yes |
| <a name="input_owner"></a> [owner](#input\_owner) | Team accountable for responding to this service's alerts | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Google Cloud metrics-scope project that owns the service, SLOs, alerts, and dashboard | `string` | n/a | yes |
| <a name="input_runbook_url"></a> [runbook\_url](#input\_runbook\_url) | HTTPS responder runbook linked from every alert and the service dashboard | `string` | n/a | yes |
| <a name="input_service_display_name"></a> [service\_display\_name](#input\_service\_display\_name) | Human-readable service name used in Monitoring and notifications | `string` | n/a | yes |
| <a name="input_service_id"></a> [service\_id](#input\_service\_id) | Stable Cloud Monitoring custom-service ID | `string` | n/a | yes |
| <a name="input_signal_alerts"></a> [signal\_alerts](#input\_signal\_alerts) | Bounded metric-threshold alerts for correctness, freshness, saturation, and dependency signals | <pre>map(object({<br/>    display_name            = string<br/>    filter                  = string<br/>    comparison              = string<br/>    threshold_value         = number<br/>    duration                = optional(string, "60s")<br/>    severity                = optional(string, "WARNING")<br/>    alignment_period        = optional(string, "60s")<br/>    per_series_aligner      = optional(string, "ALIGN_MAX")<br/>    cross_series_reducer    = optional(string, "REDUCE_MAX")<br/>    group_by_fields         = optional(set(string), [])<br/>    evaluation_missing_data = optional(string, "EVALUATION_MISSING_DATA_ACTIVE")<br/>    trigger_count           = optional(number, 1)<br/>    minimum_samples = optional(object({<br/>      filter               = string<br/>      threshold_value      = number<br/>      duration             = optional(string, "60s")<br/>      alignment_period     = optional(string, "60s")<br/>      per_series_aligner   = optional(string, "ALIGN_MAX")<br/>      cross_series_reducer = optional(string, "REDUCE_SUM")<br/>      group_by_fields      = optional(set(string), [])<br/>    }), null)<br/>  }))</pre> | `{}` | no |
| <a name="input_slos"></a> [slos](#input\_slos) | Request-based SLO contracts and paired fast/slow error-budget burn windows | <pre>map(object({<br/>    display_name         = string<br/>    goal                 = number<br/>    rolling_period_days  = optional(number, 28)<br/>    good_service_filter  = string<br/>    total_service_filter = string<br/>    fast_burn = optional(object({<br/>      threshold      = optional(number, 14.4)<br/>      short_lookback = optional(string, "300s")<br/>      long_lookback  = optional(string, "3600s")<br/>    }), {})<br/>    slow_burn = optional(object({<br/>      threshold      = optional(number, 6)<br/>      short_lookback = optional(string, "1800s")<br/>      long_lookback  = optional(string, "21600s")<br/>    }), {})<br/>  }))</pre> | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_dashboard_name"></a> [dashboard\_name](#output\_dashboard\_name) | Cloud Monitoring dashboard resource name |
| <a name="output_fast_burn_alert_policy_names"></a> [fast\_burn\_alert\_policy\_names](#output\_fast\_burn\_alert\_policy\_names) | Fast-burn alert-policy resource names by objective ID |
| <a name="output_runbook_url"></a> [runbook\_url](#output\_runbook\_url) | Responder runbook linked from the module's alerts and dashboard |
| <a name="output_service_id"></a> [service\_id](#output\_service\_id) | Cloud Monitoring custom-service ID |
| <a name="output_service_name"></a> [service\_name](#output\_service\_name) | Fully qualified Cloud Monitoring custom-service resource name |
| <a name="output_signal_alert_policy_names"></a> [signal\_alert\_policy\_names](#output\_signal\_alert\_policy\_names) | Metric-threshold alert-policy resource names by governed signal ID |
| <a name="output_slo_contracts"></a> [slo\_contracts](#output\_slo\_contracts) | Reviewable goals, windows, and burn thresholds without metric-filter payloads |
| <a name="output_slo_names"></a> [slo\_names](#output\_slo\_names) | Fully qualified Cloud Monitoring SLO resource names by objective ID |
| <a name="output_slow_burn_alert_policy_names"></a> [slow\_burn\_alert\_policy\_names](#output\_slow\_burn\_alert\_policy\_names) | Sustained-burn alert-policy resource names by objective ID |
<!-- END_TF_DOCS -->
