# GitOps deployment contracts

This directory is a production-oriented, fail-closed Argo CD source contract. It is not evidence
that Argo CD, a repository credential, an operator, or a Mindclade workload is installed. Every
`Application` is paused with `argocd.argoproj.io/skip-reconcile: "true"`, uses the invalid
`SET_EXACT_40_CHAR_COMMIT_SHA` revision, and disables automatic sync, prune, and self-heal.

The only source repository is `https://github.com/mindclade-org/mindclade.git`. Repository
credentials and runtime TLS Secrets are intentionally absent and remain owned by controlled
live-cluster systems.

## Ownership and layout

- `argocd/projects.yaml` separates bootstrap, workload foundation, operator namespaces,
  operator CRDs, each controller, operator observability, and ML resources into nine exact
  AppProjects. The root Application cannot manage AppProjects.
- `bootstrap/<environment>` selects exactly one cluster environment and renders eleven paused
  child Applications. There is no all-environment root.
- `environments/<environment>` composes the corresponding Kubernetes overlay without a
  top-level namespace transformer.
- `vendor/cert-manager/v1.19.1` is the deployment source for the locked cert-manager static
  release. Its raw CRD and controller inventories are generated, disjoint, and together normalize
  to all 49 upstream objects. The final controller overlay removes shared Namespace ownership,
  pins three images by digest, and adds HA/resource/PDB controls.
- `platform/{jobset,kueue}/chart` contains repository-locked wrapper charts. Deterministic
  downstream archives add explicit CRD/controller phase controls and protect every CRD from
  Argo prune/delete.
- `argocd/bootstrap/argocd.lock.yaml` records upstream and generated cert-manager bytes,
  normalized inventory, paths, and image digests. `argocd/repositories.yaml` is a non-operative,
  non-secret repository contract.

## Intended transaction graph

| Wave | Paused Application | Required manual health gate |
| ---: | --- | --- |
| `-80` | operator namespace foundation | three PSS-restricted operator namespaces, default deny, and reviewed provider-specific network allowances |
| `-70` | cert-manager CRDs | all six CRDs `Established`; stored-version review complete |
| `-60` | cert-manager controller | three deployments available; webhook endpoints and CA injection healthy |
| `-50` | JobSet CRD | JobSet CRD `Established` |
| `-40` | JobSet controller | deployment, certificate, and exact webhook endpoints healthy |
| `-30` | Kueue CRDs | all eleven CRDs `Established`; conversion CA populated |
| `-20` | Kueue controller | deployment, webhooks, and visibility APIServices healthy |
| `-10` | operator observability | external collector auth/trust/RBAC/network qualified; PodMonitoring/Rules accepted and producing expected targets/signals |
| `0` | workload foundation | namespace/admission/quota policy healthy; workload activation remains blocked |
| `20` / `22` | Kueue resources / JobSet canary | queues remain held at zero; canary remains suspended |

These waves record the required operator sequence; they are not a cross-Application transaction
engine. This repository does not assume an Argo CD version, Application health customization, or
ApplicationSet progressive-sync capability. A release operator must remove one pause, manually
sync only that child, capture the connected health gate, and then proceed. A wave annotation alone
does not authorize or prove the next phase.

CRD Applications use server-side apply and disable client-side migration. CRDs carry
`Prune=false,Delete=false`, all applications keep automated prune disabled, and CA drift ignores
name one exact object and one exact CA field. No `Force`, `Replace`, or missing-CRD dry-run
bypass is permitted.

## Activation and adoption gates

A controlled live overlay must select one environment and one reviewed lowercase 40-character
commit. Never substitute `HEAD`, a branch, mutable tag, or semver range. Before any child is
unpaused, qualify the destination context, exact AppProjects, external repository credential,
operator-network allowances, rollback commit, alerts, and owners described in
[RUNBOOK.md](RUNBOOK.md) and [PRODUCTION_READINESS.md](PRODUCTION_READINESS.md).

Operator namespaces use the distinct `platform-operator` admission class. They retain restricted
PSS but do not select workload policies that forbid Kubernetes API tokens: cert-manager, JobSet,
and Kueue require narrowly scoped controller credentials. Connected qualification must audit the
rendered upstream RBAC, token audience/rotation, actual API calls, and default-deny exceptions.
This is not permission to schedule user workloads in operator namespaces.

Operator observability is also an external-identity gate. Before wave `-10`, a controlled
environment must supply rotating TokenRequest bearer credentials, CA-only trust material, the
JobSet client certificate, exact collector Secret-read and `*/metrics` reader bindings, and the
reviewed NetworkPolicy allowance. The paused source owns none of those Secrets or bindings.

Canonical Helm release identities are `jobset` and `kueue`. If any target cluster already has
`mindclade-jobset`, `mindclade-kueue`, or another release owning the rendered resources, stop.
Adoption requires an explicit inventory, ownership-transfer, rollback, and outage plan; GitOps
must never silently rename or take over a live Helm release.

Argo CD itself remains outside its reconciliation boundary. The controlled live repository must
supply an exact Argo CD chart/archive/provenance lock, install its CRDs before controllers, and
qualify its HA, backup, health, and security configuration. This repository does not guess that
deployment-specific pin.

## Offline validation

Run from the repository root in the pinned tool environment:

```bash
nix develop .#ci --command tools/dev/bazelw test \
  //infra/gitops:validate --test_output=errors
```

The validator invokes no cluster API and fetches no release artifact. It renders every GitOps
Kustomize root, checks paused/exact-revision and exact project/phase contracts, verifies the
cert-manager generated locks and normalized union, asserts operator namespaces cannot select
standard workload VAPs, and proves JobSet/Kueue CRD/controller disjointness plus full-render
parity.
