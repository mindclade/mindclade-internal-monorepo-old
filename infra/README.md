<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Architecture](../docs/README.md) · [Maturity](../SCAFFOLD_STATUS.md)

# Infrastructure

> **Maturity:** Foundation modules are materialized; live deployment remains
> gated by subsystem production-readiness evidence.
> **Primary implementation:** Terraform, Kubernetes, GitOps, and policy.

`infra/` declares Mindclade's cloud and deployment foundations: environments,
identity, networking, storage, data services, clusters, security controls,
observability, and GitOps composition.

## What's here

| Path | Responsibility |
| --- | --- |
| [`terraform/`](terraform/) | Reusable Google Cloud modules and environment composition |
| [`kubernetes/`](kubernetes/) | Platform controllers, policies, and workload contracts |
| [`gitops/`](gitops/) | Declarative delivery composition and promotion boundaries |
| [`observability/`](observability/) | Metrics, logs, traces, dashboards, and alert contracts |
| [`security/`](security/) | Infrastructure security controls and supporting policy |

## Boundary

- Infrastructure declares provider resources and deployment composition; it
  does not own product or scientific behavior.
- Deployable processes and provider construction belong in
  [`services/`](../services/).
- Workload identity and tenant boundaries must remain explicit and least
  privilege.
- Live activation requires reviewed environment state and cannot be inferred
  from a passing module test.

## Start here

- [`terraform/README.md`](terraform/README.md) for module and environment rules
- [`kubernetes/README.md`](kubernetes/README.md) for platform and workload
  composition
- [`gitops/README.md`](gitops/README.md) for declarative delivery
- [GCP Terraform architecture review](../docs/architecture/gcp-terraform-well-architected-review.md)

## Promotion bar

Before activation, require named ownership, reviewed contracts, policy and
security tests, explicit failure and rollback behavior, connected-provider
qualification, and current `PRODUCTION_READINESS.md` evidence. The Kubernetes
and GitOps source contracts are indexed by
[`kubernetes/PRODUCTION_READINESS.md`](kubernetes/PRODUCTION_READINESS.md) and
[`gitops/PRODUCTION_READINESS.md`](gitops/PRODUCTION_READINESS.md).
