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
identity, and applies an environment-specific capacity ceiling. The foundation
owns `mindclade-system` plus three isolated capacity domains:

| Namespace | Workload class | Queue | Initial capacity |
|---|---|---|---|
| `mindclade-batch-cpu` | CPU ingestion and preprocessing | `mindclade-batch-cpu` | zero and held |
| `mindclade-training-h100` | H100 training | `mindclade-training-h100` | zero and held |
| `mindclade-training-b200` | B200 training | `mindclade-training-b200` | zero and held |

`versions.env` records the platform compatibility tuple. GitOps composition
belongs under `infra/gitops/`; a live cluster must not be mutated directly when
GitOps owns the target.

First-party resources remain Kustomize-owned. Helm is used only by the
dependency-only wrappers under `platform/kueue/chart` and `platform/jobset/chart`.
Those wrappers pin exact upstream chart bytes and controller image digests,
carry reviewed production values and schemas, and expose separate CRD and
controller phases. The cert-manager release is an independently locked static
upstream manifest split into CRD and controller phases. CRDs are protected from
prune/delete and rendered with server-side apply; their controllers can be
rolled back without deleting stored custom resources. Helm is not a second
source of truth for first-party manifests.

The GitOps transaction is deliberately linear:

```text
cert-manager CRDs -> cert-manager controller
  -> JobSet CRDs -> JobSet controller
  -> Kueue CRDs -> Kueue controller
  -> operator telemetry -> held queue resources
```

Every Application is paused at an exact-revision placeholder. Sync waves record
the intended order; they do not make separate Applications transactional. A
release operator advances one phase at a time only after connected staging
proves API discovery, CRD establishment, webhook readiness, upgrade, and
rollback for the preceding phase.

## Fail-closed invariants

Every environment currently renders all of the following:

- namespace `mindclade-system` with the Kubernetes `restricted` Pod Security
  Standard pinned to the qualified Kubernetes minor;
- default and suspended-workload ServiceAccounts with API-token automount
  disabled and no RBAC;
- no Role, ClusterRole, RoleBinding, or ClusterRoleBinding;
- default-deny ingress and egress, with DNS as the only egress exception;
- zero Pod, Deployment, StatefulSet, DaemonSet, Job, JobSet, CronJob, PVC, GPU,
  LoadBalancer, and NodePort quota;
- workload-class-specific, min/max-only LimitRanges for CPU, H100, and B200;
- coherent Kueue resource groups: CPU, memory, ephemeral storage, Pod count,
  and GPU (for training) always receive one compatible node flavor;
- separate one-GPU packed and eight-GPU full-node training templates, both
  suspended and topology-aware;
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
| `platform/` | Transactional upstream controllers, held queue policy, native admission, GMP telemetry, and fail-closed hardware/runtime contracts |
| `planes/` | Gateway API routing and plane-specific workloads |
| `tests/` | Offline rendering, schema, policy, and invariant checks |

Services are composition roots. Cross-language contracts remain under
`protocols/`; durable orchestration policy remains under `control/`; scientific
and model semantics remain in their owning Python/Rust packages.

Every standalone service and durable worker now has an explicit module under
`services/`. The Python `model_worker` is intentionally not a standalone
Kubernetes workload: it is supervised inside the `runtime-host` Pod and uses a
local Unix-domain IPC boundary. Batch inference, reference building, and
simulation are suspended Job templates with dedicated identities and default-deny
network policy; like the other scientific workers, they cannot activate until
their engine and Rust/Python bridge have connected evidence.

## Local validation

Validation is static and does not require a kubeconfig. Bazel is the canonical
entry point; CI enters the pinned Nix closure, then the Bazel action receives
its tools and fixed-hash schemas as declared inputs:

```bash
nix develop .#ci --command tools/dev/bazelw test \
  //infra/kubernetes:validate --test_output=errors
```

The validator inventories every Kustomize, static-manifest, and Helm phase,
verifies artifact and controller digests, proves phase union/disjointness,
renders all environments, checks object scope, selectors, target ports, quota
and queue relationships, validates core and custom resources against fixed-hash
schemas, asserts the exact fail-closed admission contract, runs Conftest, and
checks GMP recording rules with `promtool`. It performs no cluster operation.

Before any live diff, follow `RUNBOOK.md`, identify the exact context and
namespace, and verify that GitOps is not the active writer. Applying, deleting,
scaling, installing CRDs, or upgrading a release requires explicit approval.

## Promotion evidence

- `PRODUCTION_READINESS.md` records the activation gates and current non-claims.
- `RUNBOOK.md` defines render, diff, rollout, incident, and rollback procedures.
- `MLOPS.md` defines model-serving, data-quality, pipeline, and monitoring
  requirements for ML workloads.
