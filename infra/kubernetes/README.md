# Kubernetes deployment foundation

**Current state:** the namespace and policy foundation is implemented and
renderable; application workloads remain deliberately blocked. This directory
does not claim that any service is production-ready or authorize a cluster
deployment.

## Source of truth

First-party manifests use Kustomize. The deployable roots are:

```text
infra/kubernetes/overlays/development
infra/kubernetes/overlays/staging
infra/kubernetes/overlays/production
```

Each root composes `base/` and `policies/`, adds an immutable environment
identity, and applies an environment-specific capacity ceiling. `versions.env`
records the platform compatibility tuple. GitOps composition belongs under
`infra/gitops/`; a live cluster must not be mutated directly when GitOps owns
the target.

First-party resources remain Kustomize-owned. Helm is used only by the
dependency-only wrappers under `platform/kueue/chart` and `platform/jobset/chart`.
Those wrappers pin an exact upstream OCI chart and controller image digest, carry
reviewed production values and a values schema, and keep controller/CRD lifecycle
separate from Mindclade custom resources. `Chart.lock` plus the vendored chart
archive makes offline rendering deterministic. Helm is not a second source of
truth for first-party manifests.

## Fail-closed invariants

Every environment currently renders all of the following:

- namespace `mindclade-system` with the Kubernetes `restricted` Pod Security
  Standard pinned to the qualified Kubernetes minor;
- default and suspended-workload ServiceAccounts with API-token automount
  disabled and no RBAC;
- no Role, ClusterRole, RoleBinding, or ClusterRoleBinding;
- default-deny ingress and egress, with DNS as the only egress exception;
- zero Pod, Deployment, StatefulSet, DaemonSet, Job, CronJob, PVC, GPU,
  LoadBalancer, and NodePort quota;
- bounded defaults and maxima through a LimitRange;
- a non-secret ConfigMap that says workload activation is blocked.

These are defense-in-depth controls, not an activation mechanism. Raising quota
without materializing service identities, least-privilege RBAC, network flows,
digest-pinned images, probes, resource envelopes, rollout policy, and release
evidence is prohibited.

No secret value, kubeconfig, token, private key, cluster endpoint, or cloud
credential belongs in this tree. Manifests may reference a separately managed
Secret or external-secret object only after that integration is qualified.

## Directory responsibilities

| Path | Responsibility |
|---|---|
| `base/` | Namespace identity, safe defaults, and cluster-independent primitives |
| `policies/` | Fail-closed network and resource admission boundaries |
| `overlays/` | Environment identity and non-activating capacity ceilings |
| `services/` | Per-deployable workload, identity, Service, PDB, and network policy |
| `workloads/` | Durable Job/JobSet templates for ingestion, preprocessing, and training |
| `platform/` | Locked upstream controllers, held queue policy, native admission, and fail-closed hardware/runtime contracts |
| `planes/` | Gateway API routing and plane-specific workloads |
| `tests/` | Offline rendering, schema, policy, and invariant checks |

Services are composition roots. Cross-language contracts remain under
`protocols/`; durable orchestration policy remains under `control/`; scientific
and model semantics remain in their owning Python/Rust packages.

## Local validation

Validation is offline and does not require a kubeconfig:

```bash
nix develop .#ci --command bash infra/kubernetes/tests/validate.sh
tools/dev/bazelw test //infra/kubernetes:validate --test_output=errors
```

The validator inventories every Kustomize and Helm root, verifies vendored
artifact and controller digests, renders all environments, checks object scope
and CRD structure, applies strict Kubernetes 1.36 schemas, and runs the
fail-closed Conftest policy. It performs no network or cluster operation.

Before any live diff, follow `RUNBOOK.md`, identify the exact context and
namespace, and verify that GitOps is not the active writer. Applying, deleting,
scaling, installing CRDs, or upgrading a release requires explicit approval.

## Promotion evidence

- `PRODUCTION_READINESS.md` records the activation gates and current non-claims.
- `RUNBOOK.md` defines render, diff, rollout, incident, and rollback procedures.
- `MLOPS.md` defines model-serving, data-quality, pipeline, and monitoring
  requirements for ML workloads.
