# Infra / Observability

- **Status:** Contracts materialized; environment activation remains blocked.
- **Primary implementation ownership:** Google Managed Service for Prometheus collection and
  recording rules in Kubernetes; Cloud Monitoring SLOs, alerts, channels, and dashboards in
  Terraform.

## Purpose

Deployment and cloud foundations. Infrastructure declares environments, workload identity, storage, databases, queues, clusters, security policy, observability, and GitOps composition. This path specializes that domain for **observability**.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Implemented contract

Operator metrics are declared under `infra/kubernetes/platform/observability`. Those manifests use
namespaced `PodMonitoring` and `Rules` resources and never install Prometheus Operator,
Alertmanager, or a second metrics backend. Kueue and JobSet Helm `ServiceMonitor` generation stays
disabled.

Files under `alerts/` are provider-neutral design contracts, not Kubernetes resources. Every
contract uses `mindclade.dev/cloud-monitoring-alert-contract/v1alpha1` and is checked against
`alert-contract.schema.json` plus the semantic catalog validator. Configurable SLI defaults live
in `availability-profiles.yaml` and remain `proposed` and disabled by default. Environment
translation requires distinct Google Chat and email channel resource names, owners, runbooks,
reviewed thresholds, and evidence. No `AlertPolicySet` CRD exists or is expected.

`jobset_outcomes.py` implements a bounded, durable, idempotent JobSet terminal-outcome ledger and
OpenMetrics exposition without JobSet-name or UID labels. It is source behavior, not a deployed
watcher: Kubernetes watch identity, RBAC, storage, Service/PodMonitoring, restart/relist behavior,
and synthetic connected evidence remain environment-owned activation gates.

`alerts/control-admission-degraded.yaml` and `dashboards/control-plane.json` define the disabled
99.95-percent availability, exact 100-millisecond latency compliance, diagnostic p99, paired
5m+1h/30m+6h error-budget burn, expiration, sweep, and audit/outbox drift contracts. Their source
GMP monitor and recording rules live with the control-plane workload under
`infra/kubernetes/services/control-plane`. The base grants no metrics ingress and does not guess a
managed collector identity. API and maintenance metrics have source-owned private listeners;
maintenance sampling stays off the scrape path, is timeout-bounded, and reads only indexed recent
state. Exact per-replica series inventories prevent partial exporters from silently shrinking the
SLI. Connected migration receipts, representative volume, GMP, and alert evidence still block
activation.

## Promotion requirements

Before an observability contract is enabled in an environment, require:

- a named owner, HTTPS runbook, notification channels, and reviewed SLI/SLO thresholds;
- bounded scrape samples and label cardinality with no tenant, model, dataset, prompt, feature,
  label, or request identifiers;
- successful GMP configuration and target status, a synthetic metric query, and rule evaluation;
- Cloud Monitoring alert fire-and-resolve evidence through a non-production channel;
- an exact NetworkPolicy allowance for the observed managed collector identity; and
- rollback evidence proving that disabling collection leaves workloads and controllers healthy.

Correctness alerts use the minimum `1m` retest because missing-data evaluation requires a duration
of at least 60 seconds in Cloud Monitoring. Every duration remains a positive whole number of
minutes, hours, or days; scrape and rule-evaluation latency is accounted for separately.

See the architecture chapter for this domain and `SCAFFOLD_STATUS.md` for the
artifact-wide implementation status.

Current evidence and remaining connected-environment blockers are tracked in
[`PRODUCTION_READINESS.md`](PRODUCTION_READINESS.md).
