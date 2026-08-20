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

Files under `alerts/` are provider-neutral design contracts, not Kubernetes resources. In
particular, `studio-browser-plane.yaml` uses
`mindclade.dev/cloud-monitoring-alert-contract/v1alpha1`; it must be translated into inputs for
`infra/terraform/modules/monitoring` by an environment repository after owners, channels,
runbooks, and SLO thresholds are approved. No `AlertPolicySet` CRD exists or is expected.

## Promotion requirements

Before an observability contract is enabled in an environment, require:

- a named owner, HTTPS runbook, notification channels, and reviewed SLI/SLO thresholds;
- bounded scrape samples and label cardinality with no tenant, model, dataset, prompt, feature,
  label, or request identifiers;
- successful GMP configuration and target status, a synthetic metric query, and rule evaluation;
- Cloud Monitoring alert fire-and-resolve evidence through a non-production channel;
- an exact NetworkPolicy allowance for the observed managed collector identity; and
- rollback evidence proving that disabling collection leaves workloads and controllers healthy.

See the architecture chapter for this domain and `SCAFFOLD_STATUS.md` for the
artifact-wide implementation status.

Current evidence and remaining connected-environment blockers are tracked in
[`PRODUCTION_READINESS.md`](PRODUCTION_READINESS.md).
