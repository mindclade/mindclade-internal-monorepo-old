# control.admission SLO

`control.admission` is the authoritative entitlement and budget decision boundary for one
external AI Gateway request. Production activation remains blocked until a durable repository,
atomic audit/outbox writes, reconciliation, metrics, and connected failure testing satisfy this
contract.

## Service objectives

- At least 99.95% of well-formed decisions complete within the monthly availability window;
  policy denials and budget exhaustion are successful decisions, not availability failures.
- p99 decision latency is at most 100 ms at the service boundary, excluding caller cancellation.
- No request is authorized against a stale policy epoch, an inactive policy window, an expired
  reservation, a mismatched idempotency fingerprint, or quota beyond the locked budget.
- Duplicate retries produce exactly one reservation and one terminal accounting outcome.
- An expired reservation never authorizes commit or replay, even before the asynchronous ledger
  transition is materialized. Under a healthy maintenance lease and queue, expired reservations
  are materialized within 15 seconds of `expires_at`; durable ledger/audit/outbox divergence is
  zero.

Correctness objectives have a zero error budget. Any overspend, stale-policy authorization,
expired replay, identity collision, or missing audit/outbox transition stops promotion and enters
the incident procedure. Availability must never be restored by bypassing admission.

Measurements use only the fixed operation/result, probe, reconciliation-kind, process-role, and
service allowlists. They never include prompts, responses, credentials, routes, reasons,
providers, idempotency keys, or unbounded tenant/subject/workspace identifiers. Monthly SLO
evidence, reconciliation results, and intentional-negative tests are retained with release
qualification.

Production telemetry must expose bounded expiration backlog size, oldest expired-but-reserved age,
last successful sweep time, and audit/outbox reconciliation drift. Alert when the oldest backlog
age exceeds 15 seconds, any backlog remains after two successful sweeps, or reconciliation drift
is non-zero. Source tests do not qualify these measurements; activation requires connected
scrape, synthetic expiry, alert-fire, and alert-resolution evidence.

## Telemetry contract

The service-owned raw families and workload-owned GMP recordings are mapped explicitly below.
Provider-neutral names are consumed by the alert and dashboard contracts; they are not a second
metrics backend.

| Provider-neutral signal | Raw Prometheus family or target | GMP recording rule |
|---|---|---|
| `mindclade.control_admission.api_metric_contract_complete` | exact API series inventory per desired replica | `mindclade:control_admission_api_metric_contract_complete:min` |
| `mindclade.control_admission.api_target_up` | service-scoped API `up` plus desired Deployment replicas | `mindclade:control_admission_api_target_up:min` |
| `mindclade.control_admission.decision_events_5m` | `mindclade_control_admission_decisions_total` | `mindclade:control_admission_decision_events:increase5m` |
| `mindclade.control_admission.error_budget_fast_burn_pair` | decision counter over 5m and 1h | `mindclade:control_admission_error_budget_fast_burn:paired` |
| `mindclade.control_admission.error_budget_slow_burn_pair` | decision counter over 30m and 6h | `mindclade:control_admission_error_budget_slow_burn:paired` |
| `mindclade.control_admission.decision_latency_objective_ratio_5m` | histogram `le="0.1"` compliance | `mindclade:control_admission_decision_latency_objective:ratio5m` |
| `mindclade.control_admission.decision_p99_seconds` | `mindclade_control_admission_decision_duration_seconds` | `mindclade:control_admission_decision_seconds:p99_rate5m` |
| `mindclade.control_admission.expiration_backlog` | bounded backlog: 0–1000 exact, 1001 means at least 1001 | `mindclade:control_admission_expiration_backlog:max` |
| `mindclade.control_admission.oldest_expired_reservation_age_seconds` | `mindclade_control_admission_oldest_expired_reservation_age_seconds` | `mindclade:control_admission_oldest_expired_reservation_age_seconds:max` |
| `mindclade.control_admission.last_successful_sweep_age_seconds` | `mindclade_control_admission_last_successful_sweep_timestamp_seconds` | `mindclade:control_admission_last_successful_sweep_age_seconds:max` |
| `mindclade.control_admission.consecutive_backlogged_sweeps` | `mindclade_control_admission_consecutive_backlogged_sweeps` | `mindclade:control_admission_consecutive_backlogged_sweeps:max` |
| `mindclade.control_admission.event_drift` | `mindclade_control_admission_event_drift` | `mindclade:control_admission_event_drift:max` |
| `mindclade.control_admission.expiration_snapshot_age_seconds` | maintenance snapshot timestamp with `probe="expiration"` | `mindclade:control_admission_expiration_snapshot_age_seconds:max` |
| `mindclade.control_admission.lineage_snapshot_age_seconds` | maintenance snapshot timestamp with `probe="lineage"` | `mindclade:control_admission_lineage_snapshot_age_seconds:max` |
| `mindclade.control_admission.snapshot_success` | `mindclade_control_admission_snapshot_success` | `mindclade:control_admission_snapshot_success:min` |
| `mindclade.control_admission.maintenance_metric_contract_complete` | exact maintenance series inventory per desired replica | `mindclade:control_admission_maintenance_metric_contract_complete:min` |
| `mindclade.control_admission.maintenance_target_up` | service-scoped maintenance `up` plus desired Deployment replicas | `mindclade:control_admission_maintenance_target_up:min` |

Decision `operation` is restricted to `admit`, `commit`, and `release`. Decision `result` is
restricted to `allow`, `deny`, `exhausted`, `conflict`, `not_found`, `unavailable`, `internal`,
`deadline`, `canceled`, and `invalid`. Caller-owned `canceled` decisions are omitted from both
availability and latency populations. Only `unavailable`, `internal`, and `deadline` consume
availability error budget; denial and exhaustion remain successful policy decisions. Reconciliation `kind`,
sampler `probe`, and process `role` are fixed allowlists. Tenant, workspace, subject, route,
provider, model, prompt, request, idempotency, and reservation identifiers are forbidden labels.

Decision counters and histogram buckets sum across API replicas. Target health uses the minimum
service-scoped `up` value and requires the discovered target count to equal the desired Deployment
replica count; an unrelated role label, failed scrape, or missing replica therefore cannot be
hidden by one healthy target. The API v1 metric contract additionally requires exactly 30
decision counters, 36 histogram buckets, three histogram counts, and three histogram sums per
desired replica. Maintenance backlog, oldest age, drift, and consecutive-backlog
values use the conservative maximum. Sweep and probe age use the minimum reported success
timestamp, which yields the oldest replica age, while snapshot success uses the minimum across
replicas. The maintenance v1 metric contract requires four scalar series, all three drift kinds,
both snapshot timestamps, and both snapshot outcomes per desired replica. These completeness
rules prevent a successfully scraped but partially instrumented replica from disappearing from
the SLI.

Fast burn is the minimum of the 5-minute and 1-hour burn rates; slow burn is the minimum of the
30-minute and 6-hour rates. A paired value breaches its threshold only when both governed windows
breach. The latency alert uses the exact share at or below 100 milliseconds and remains healthy at
exactly 99 percent; interpolated p99 is diagnostic only.

Correctness thresholds are retested for one minute because Cloud Monitoring requires at least 60
seconds when missing-data evaluation is configured. Scrape and rule-evaluation intervals add to
that provider minimum, so the alert-delivery objective is distinct from the 15-second state
materialization objective. API and maintenance families have service owners and private
listeners, but their Rules, alerts, and dashboard remain activation-blocked until v14 migration
receipts, representative PostgreSQL volume, connected GMP evaluation, and synthetic delivery are
qualified.
