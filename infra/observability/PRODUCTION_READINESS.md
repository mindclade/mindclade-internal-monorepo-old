# Observability production readiness

**Decision:** operator telemetry contracts implemented; collection and paging activation blocked.
**Last repository review:** 2026-08-20.

## Repository evidence

| Gate | Status | Evidence or blocker |
|---|---|---|
| GKE managed collection and DCGM enabled | PASS | Terraform GKE module and tests |
| Kueue and JobSet TLS scrape contracts | PARTIAL | `PodMonitoring` declares external bearer/trust/client-cert contracts; credentials, exact RBAC, rotation, and reload are environment blockers |
| Bounded metric families and labels | PARTIAL | Metric-family filters, aggregate recording rules, and scrape limits exist; connected volume, cost, and protected-label behavior still require qualification |
| Namespaced recording rules | PASS | GMP `Rules` resources and promtool validation |
| Durable JobSet outcome signal | BLOCKED | Upstream per-name terminal counters are unsuitable for windowed outcomes; a condition/event exporter and failure/completion fixtures are required before capacity |
| Kubernetes Alertmanager or `ServiceMonitor` dependency | PASS | Deliberately absent |
| Cloud Monitoring ownership | PASS | Terraform monitoring module and alert contracts |
| Exact collector NetworkPolicy identity | BLOCKED | Must be observed in each qualification cluster |
| Scrape authorization and trust | BLOCKED | Rotating TokenRequest tokens, exact metrics-reader bindings, CA-only trust publication, JobSet client certificate, and Secret-read scoping require environment ownership |
| GMP target and rule status | BLOCKED | Requires connected GKE evidence |
| SLO thresholds, channels, owners, and runbooks | BLOCKED | Required environment inputs are not stored here |
| Synthetic alert fire and resolve | BLOCKED | Must use a non-production notification channel |

The source tree does not claim that the managed collector can reach either controller. Operator
Applications and their monitoring Application remain paused until a live overlay supplies the
reviewed collector identity and the connected gate proves TLS, target freshness, query results,
rule evaluation, and notification delivery.

Cloud Monitoring alert policies remain disabled outside a controlled environment repository. The
repository never commits channel credentials, service-account keys, TLS private keys, or an
Alertmanager configuration Secret.
