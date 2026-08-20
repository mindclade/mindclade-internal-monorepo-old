# Infra

- **Status:** Foundation modules are materialized, but live production deployment remains gated
  by each subsystem's production-readiness evidence.
- **Primary implementation ownership:** Terraform, Kubernetes, GitOps, and policy configuration

## Purpose

Deployment and cloud foundations. Infrastructure declares environments, workload identity, storage, databases, queues, clusters, security policy, observability, and GitOps composition. This path specializes that domain for **infra**.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Promotion requirements

Before an infrastructure subsystem is activated in a live environment, require:

- a named owner and reviewed stable contract;
- implementation with bounded resources, cancellation, and deterministic or
  explicitly statistical behavior;
- package-local tests plus required integration/numerical/security evidence;
- a Bazel target using the pinned Nix toolchain environment;
- explicit inputs, outputs, compatibility, failure, retry, and rollback rules;
- documentation of limits and non-responsibilities;
- `PRODUCTION_READINESS.md` evidence for deployment-facing code.

Kubernetes and GitOps source contracts are documented in
`kubernetes/PRODUCTION_READINESS.md` and `gitops/PRODUCTION_READINESS.md`. See
the architecture chapter for this domain and `SCAFFOLD_STATUS.md` for the
artifact-wide implementation status.
