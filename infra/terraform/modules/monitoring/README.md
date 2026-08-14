# Cloud Monitoring service contract

This module creates one custom service, one or more request-based SLOs, paired
multi-window error-budget burn alerts, and an SLO dashboard. Every alert has an
owner, severity, HTTPS runbook link, open/close notifications, short and long
lookback conditions, and at least one pre-existing notification channel.
Monitoring resources have both provider- and Terraform-level deletion guards.
Dashboard SLO queries use an explicit 60-second alignment period so their
non-`ALIGN_NONE` aligners satisfy the Cloud Monitoring API contract.
Burn-rate lookbacks are normalized to seconds and must remain between one
minute and 24 hours, with short windows before long windows and fast signals
ordered ahead of their slow counterparts.

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
      goal         = 0.999
      good_service_filter  = "metric.type=\"custom.googleapis.com/http/good_requests\" AND resource.type=\"generic_task\""
      total_service_filter = "metric.type=\"custom.googleapis.com/http/requests\" AND resource.type=\"generic_task\""
    }
  }
}
```

Enable `monitoring.googleapis.com` before use and manage notification channels in
a separate sensitive lifecycle. After a reviewed plan, validate SLO math against
known traffic, fire and recover a synthetic alert through every route, inspect
dashboard data, test deduplication/escalation, and confirm missing telemetry is
detected independently. The default 14.4x/5m+1h and 6x/30m+6h burn windows are
starting contracts, not substitutes for approved business objectives. This
module is configuration, not evidence that telemetry or response is operational.
