# GitOps production readiness

**Repository posture:** fail-closed source contract implemented; live deployment unqualified.
**Last repository review:** 2026-08-20.
**Owner:** platform engineering with security and ML platform review.

Every child is paused at an invalid revision. Source presence is not installation, health, or
production-readiness evidence.

## Implemented controls

| Control | Repository evidence |
| --- | --- |
| Immutable and paused source | Exact-commit placeholder; no branch/tag/range; automatic sync, prune, and self-heal disabled |
| Governance separation | Nine exact AppProjects; the root cannot manage AppProjects or Secrets |
| Transaction phases | Eleven child Applications with intended waves `-80` through `22`; CRDs and controllers have distinct sources |
| cert-manager provenance | Static v1.19.1 URL/bytes/SHA locked; generated 6/43 split is disjoint and normalizes to all 49 upstream objects |
| Helm provenance | Kueue/JobSet upstream OCI/archive and deterministic downstream archive locks; images digest pinned |
| CRD safety | CRD apps use SSA; every CRD has `Prune=false,Delete=false`; no automated prune/finalizer/force/replace |
| Drift ownership | CA ignores name one exact resource and one exact CA field; no broad manager/kind ignore |
| Operator isolation | Three restricted-PSS namespaces, explicit `platform-operator` class, and ingress/egress default deny |
| Availability and bounds | Two controller replicas/PDBs where supported; CPU, memory, and ephemeral storage are bounded |
| Workload fail-close | Workload namespaces retain standard admission, zero Pod quota/capacity, held queues, and suspended canary |
| Observability gate | Dedicated exact-kind/destination project and paused wave `-10`; external collector credentials, trust, RBAC, and network remain unqualified |
| Offline proof | GitOps validator renders all roots, verifies locks/projects/phases/parity, and performs no live call or fetch |

Wave annotations express intended order only. No Argo version, Application health customization, or
progressive-sync behavior is assumed. Transactionality requires a human-controlled manual sync and
connected health record after each child.

## Live activation blockers

Do not unpause any child until a controlled live repository records and approves:

- exact Argo CD chart/archive/provenance, rendered digest, version-compatible CRDs, HA, backup,
  restore, and controller health customization decision;
- selected environment, immutable source commit, destination context/identity, and exact
  AppProject authorization proof;
- external repository authentication owner, rotation/expiry control, and denied-access test;
- connected reproduction of cert-manager upstream bytes and generated split, plus webhook/CA
  readiness evidence;
- JobSet/Kueue vendored archive reproduction and CRD stored/conversion-version review;
- a provider-specific operator NetworkPolicy overlay for DNS, Kubernetes API, control-plane
  webhooks, probes, and metrics, with denied unexpected-flow tests;
- explicit audit of operator ServiceAccounts, upstream RBAC, API-token audience/rotation, and
  observed API calls; `platform-operator` is not a user-workload exemption;
- externally owned rotating collector TokenRequest credentials, CA-only trust, JobSet client
  certificate, exact Secret-read and `*/metrics` reader bindings, and credential reload evidence;
- reserved system-node capacity, PSS compatibility, scheduling, disruption, and upgrade evidence;
- alerts/dashboards for Argo errors/out-of-sync duration, webhook and APIService health,
  certificate expiry, queue/controller unavailability, and missing metrics targets;
- manual forward/rollback rehearsal with CRDs preserved and pruning disabled;
- platform, security, environment-owner, and ML-platform approval.

## Adoption and automation policy

The canonical Helm identities are `jobset` and `kueue`. Any existing `mindclade-*` or other
release ownership is a breaking adoption gate: stop and approve an explicit ownership migration;
never silently rename or take over resources.

Initial bootstrap and every operator phase are manual. CRD automatic prune remains forbidden.
Controller automation may be considered only after repeated observed reconciliations, qualified
health checks, rollback rehearsal, and a separate policy change. ML-resource automation remains
disabled until capacity, admission, cancellation, checkpoint/restore, numerical parity, and
observability gates pass.

## Release evidence

Attach to each promotion:

1. immutable commit plus selected-environment bootstrap/foundation render digests;
2. offline validator and connected cert-manager split-reproduction output;
3. chart/archive/image locks and phased full-render parity evidence;
4. sanitized cluster/Argo versions, AppProject authorization, RBAC/token, and NetworkPolicy tests;
5. diff and health evidence after each manually activated child;
6. proof CRDs stayed established/protected, queues stayed held at zero, and the canary stayed
   suspended;
7. tested rollback commit, recovery owner, timestamps, and incident/rollback triggers.
