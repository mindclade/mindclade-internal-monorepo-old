# Metrics-scope observability composition module

This module attaches existing projects to an existing Google Cloud metrics scope
and composes one or more instances of `../monitoring` in the scoping project. It
does not reimplement service-level objective logic: custom services, request-based
SLOs, paired fast/slow burn alerts, bounded signal alerts, dashboards, runbook links,
labels, deletion guards, and their semantic validation remain owned and tested by
that module.

Each `monitored_project_ids` entry creates a protected
`google_monitoring_monitored_project` membership. The scoping project is already
included by Google Cloud and is rejected from that set. Each `services` key is the
stable Monitoring custom-service ID; its value is passed to `../monitoring`. Scope
memberships are created before service monitoring resources so dashboards and
alerts begin against the intended project view.

## Prerequisites and responsibilities

Enable the Cloud Monitoring API in the scoping and monitored projects. The
Terraform identity needs metrics-scope membership administration on the scoping
project and the documented Monitoring permissions in each monitored project. It
also needs the resource permissions required by `../monitoring` in the scoping
project. Notification channels must already exist and route to tested responders.
Metric descriptors, log sinks, trace collection, agents, Managed Service for
Prometheus, uptime checks, telemetry sampling/redaction, and notification-channel
creation are outside this module.

Because a metrics scope can expose many projects, every composed SLO good/total
filter and every signal or minimum-sample filter must include exactly one explicit
`project = "project-id"` selector. This prevents an otherwise valid metric type from
silently aggregating unrelated projects. An intentional portfolio-wide SLO or alert
needs a separately reviewed monitoring design; it cannot bypass this module's
per-service isolation contract.

Metrics scopes increase cross-project visibility; restrict access to the scoping
project, audit scope changes, avoid sensitive label values, and apply retention and
regional controls at telemetry sources. Keep filters bounded by stable resource and
metric labels to manage cardinality and ingestion cost. Establish budgets for
metrics, logs, traces, dashboards, and alert traffic.

Both scope memberships and the monitoring resources composed beneath this module
use deletion protection. A reviewed decommission must first remove guards in code,
preserve required telemetry evidence, and assess alert/SLO gaps. Adding a project
to a scope does not prove that it emits valid telemetry. Before relying on the
result, test notification routing, dashboard queries, missing-data behavior, burn
alerts, runbooks, access control, monitored-project removal, and disaster-recovery
procedures.

Provider-mock tests validate Terraform wiring and failure contracts only. They do
not contact Cloud Monitoring, validate metric filters against real descriptors, or
prove telemetry continuity and responder readiness.

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
| <a name="input_metrics_scope_project_id"></a> [metrics\_scope\_project\_id](#input\_metrics\_scope\_project\_id) | Existing scoping project that owns the metrics scope and composed Monitoring resources | `string` | n/a | yes |
| <a name="input_monitored_project_ids"></a> [monitored\_project\_ids](#input\_monitored\_project\_ids) | Additional existing projects attached to the scoping project's metrics scope | `set(string)` | `[]` | no |
| <a name="input_services"></a> [services](#input\_services) | Service-monitoring compositions keyed by stable custom-service ID; SLO semantics are implemented by ../monitoring | <pre>map(object({<br/>    environment           = string<br/>    owner                 = string<br/>    service_display_name  = string<br/>    runbook_url           = string<br/>    notification_channels = set(string)<br/>    slos = map(object({<br/>      display_name         = string<br/>      goal                 = number<br/>      rolling_period_days  = optional(number, 28)<br/>      good_service_filter  = string<br/>      total_service_filter = string<br/>      fast_burn = optional(object({<br/>        threshold      = optional(number, 14.4)<br/>        short_lookback = optional(string, "300s")<br/>        long_lookback  = optional(string, "3600s")<br/>      }), {})<br/>      slow_burn = optional(object({<br/>        threshold      = optional(number, 6)<br/>        short_lookback = optional(string, "1800s")<br/>        long_lookback  = optional(string, "21600s")<br/>      }), {})<br/>    }))<br/>    signal_alerts = optional(map(object({<br/>      display_name            = string<br/>      filter                  = string<br/>      comparison              = string<br/>      threshold_value         = number<br/>      duration                = optional(string, "60s")<br/>      severity                = optional(string, "WARNING")<br/>      alignment_period        = optional(string, "60s")<br/>      per_series_aligner      = optional(string, "ALIGN_MAX")<br/>      cross_series_reducer    = optional(string, "REDUCE_MAX")<br/>      group_by_fields         = optional(set(string), [])<br/>      evaluation_missing_data = optional(string, "EVALUATION_MISSING_DATA_ACTIVE")<br/>      trigger_count           = optional(number, 1)<br/>      minimum_samples = optional(object({<br/>        filter               = string<br/>        threshold_value      = number<br/>        duration             = optional(string, "60s")<br/>        alignment_period     = optional(string, "60s")<br/>        per_series_aligner   = optional(string, "ALIGN_MAX")<br/>        cross_series_reducer = optional(string, "REDUCE_SUM")<br/>        group_by_fields      = optional(set(string), [])<br/>      }), null)<br/>    })), {})<br/>    labels                   = optional(map(string), {})<br/>    alert_auto_close_seconds = optional(number, 604800)<br/>  }))</pre> | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_dashboard_names"></a> [dashboard\_names](#output\_dashboard\_names) | Protected Monitoring dashboard names keyed by service ID |
| <a name="output_fast_burn_alert_policy_names"></a> [fast\_burn\_alert\_policy\_names](#output\_fast\_burn\_alert\_policy\_names) | Fast-burn alert-policy names keyed first by service ID and then objective ID |
| <a name="output_metrics_scope_name"></a> [metrics\_scope\_name](#output\_metrics\_scope\_name) | Fully qualified metrics-scope resource name |
| <a name="output_monitored_projects"></a> [monitored\_projects](#output\_monitored\_projects) | Protected metrics-scope memberships keyed by monitored project ID |
| <a name="output_runbook_urls"></a> [runbook\_urls](#output\_runbook\_urls) | Responder runbooks keyed by service ID |
| <a name="output_service_names"></a> [service\_names](#output\_service\_names) | Fully qualified custom-service resource names keyed by service ID |
| <a name="output_signal_alert_policy_names"></a> [signal\_alert\_policy\_names](#output\_signal\_alert\_policy\_names) | Metric-threshold alert-policy names keyed first by service ID and then governed signal ID |
| <a name="output_slo_contracts"></a> [slo\_contracts](#output\_slo\_contracts) | Reviewable goals, windows, and burn thresholds keyed by service and objective |
| <a name="output_slo_names"></a> [slo\_names](#output\_slo\_names) | SLO resource names keyed first by service ID and then objective ID |
| <a name="output_slow_burn_alert_policy_names"></a> [slow\_burn\_alert\_policy\_names](#output\_slow\_burn\_alert\_policy\_names) | Sustained-burn alert-policy names keyed first by service ID and then objective ID |
<!-- END_TF_DOCS -->
