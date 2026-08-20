# Metrics-scope observability composition module

This module attaches existing projects to an existing Google Cloud metrics scope
and composes one or more instances of `../monitoring` in the scoping project. It
does not reimplement service-level objective logic: custom services, request-based
SLOs, paired fast/slow burn alerts, dashboards, runbook links, labels, deletion
guards, and their semantic validation remain owned and tested by that module.

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
filter must include an explicit `project = "project-id"` selector. This prevents an
otherwise valid metric type from silently aggregating unrelated projects. An
intentional portfolio-wide SLO needs a separately reviewed monitoring design; it
cannot bypass this module's per-service isolation contract.

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
