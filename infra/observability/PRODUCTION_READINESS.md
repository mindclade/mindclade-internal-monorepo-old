# Observability production readiness

**Decision:** operator and admission telemetry contracts implemented; collection and paging activation blocked.
**Last repository review:** 2026-08-21.

## Repository evidence

| Gate | Status | Evidence or blocker |
|---|---|---|
| GKE managed collection and DCGM enabled | PASS | Terraform GKE module and tests |
| Kueue and JobSet TLS scrape contracts | PARTIAL | `PodMonitoring` declares external bearer/trust/client-cert contracts; credentials, exact RBAC, rotation, and reload are environment blockers |
| Bounded metric families and labels | PARTIAL | Metric-family filters, aggregate recording rules, and scrape limits exist; connected volume, cost, and protected-label behavior still require qualification |
| Namespaced recording rules | PASS | GMP `Rules` resources and promtool validation |
| Durable JobSet outcome signal | PARTIAL | Bounded/idempotent ledger, atomic checkpoint, aggregate OpenMetrics source, recording rules, and behavior fixtures are implemented; connected watcher/RBAC/storage/scrape and restart/relist qualification remain required before capacity |
| Control-admission decision signals | PARTIAL | Bounded decision counter/latency contracts, GMP allowlist/rules, 99.95-percent profile, alerts, dashboard, and promtool fixtures exist; connected volume and target/rule evidence remain required |
| Control-admission maintenance signals | PARTIAL | Bounded indexed expiration/lineage sampler, private listener, fixed-cardinality metrics, v14 indexes, per-replica inventory gate, and source/live PostgreSQL tests exist; connected migration receipt, representative volume, GMP target/rules, alert translation, and fire/resolve evidence remain blocked |
| Kubernetes Alertmanager or `ServiceMonitor` dependency | PASS | Deliberately absent |
| Cloud Monitoring ownership | PASS | Terraform monitoring module and alert contracts |
| Exact collector NetworkPolicy identity | BLOCKED | Must be observed in each qualification cluster |
| Scrape authorization and trust | BLOCKED | Rotating TokenRequest tokens, exact metrics-reader bindings, CA-only trust publication, JobSet client certificate, and Secret-read scoping require environment ownership |
| GMP target and rule status | BLOCKED | Requires connected GKE evidence |
| SLO thresholds, channels, owners, and runbooks | BLOCKED | Proposed configurable profiles and named source owners exist; environment approval, HTTPS runbooks, and distinct Google Chat/email channel resource names are still required |
| Synthetic alert fire and resolve | BLOCKED | Must use a non-production notification channel |

The source tree does not claim that the managed collector can reach controllers or control-plane
Pods. Operator and control-plane Applications and their monitoring resources remain paused until
a live overlay supplies the reviewed collector identity and the connected gate proves target
freshness, query results, rule evaluation, and notification delivery. Operator TLS qualification
remains an additional requirement for the authenticated controller endpoints.

Cloud Monitoring alert policies remain disabled outside a controlled environment repository. The
repository never commits channel credentials, service-account keys, TLS private keys, or an
Alertmanager configuration Secret.
