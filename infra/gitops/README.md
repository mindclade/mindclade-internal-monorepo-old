# GitOps deployment contracts

This directory is a production-oriented, fail-closed Argo CD source contract. It is not
evidence that Argo CD, a repository credential, or any Mindclade workload is installed in a
cluster. All `Application` objects are committed with reconciliation paused, automatic sync
disabled, pruning disabled, and an invalid revision placeholder.

The only permitted source repository is
`https://github.com/mindclade-org/mindclade.git`. Repository authentication is deliberately
absent: Argo CD stores repository connections in `Secret` objects, and credentials belong in
the controlled live-cluster secret system rather than this source repository.

## Layout

- `argocd/projects.yaml` declares separate bootstrap, foundation, operator, and ML-resource
  projects. Each project has exact sources, destinations, and resource-kind allowlists. Projects
  are applied as a separate governance step and cannot be rewritten by the root application.
- `argocd/app-of-apps.yaml` is the manually bootstrapped, paused root application template.
- `bootstrap/<environment>` renders the bootstrap objects for exactly one cluster
  environment. There is intentionally no root that combines all environments.
- `environments/<environment>` renders the Kubernetes foundation for that environment and
  declares its paused child applications.
- `argocd/bootstrap/argocd.lock.yaml` records byte-level upstream content locks. It is a normal
  `ConfigMap`, not an unimplemented custom resource.
- `argocd/repositories.yaml` records the non-secret repository contract. It does not register a
  repository with Argo CD.

## Activation gates

`SET_EXACT_40_CHAR_COMMIT_SHA` and `SET_ENVIRONMENT` are deliberate invalid values. A controlled
live configuration must replace them without weakening any project allowlist. Before removing
`argocd.argoproj.io/skip-reconcile: "true"`, the release owner must provide all of the following:

1. a reviewed, immutable 40-character Git commit SHA containing the rendered paths;
2. exactly one environment selected for the cluster;
3. an externally provisioned Argo CD repository credential, if the repository is private;
4. a pinned and qualified Argo CD installation plus the locked cert-manager prerequisite;
5. successful offline render, schema, Helm, and policy validation for that same commit;
6. destination-cluster identity, scope, capacity, admission, monitoring, rollback, and owner
   evidence in the controlled live repository.

Do not replace the SHA with `HEAD`, a branch, a mutable tag, or a semver range. Do not remove the
pause annotation from every application at once. Follow [RUNBOOK.md](RUNBOOK.md).

## Reconciliation order

The ordering contract is explicit even while every application is paused:

| Wave | Content | Gate |
| ---: | --- | --- |
| `-30` | externally bootstrapped cert-manager CRDs and controller | locked bytes verified; webhook healthy |
| `-20` | Kueue and JobSet CRDs/controllers from local locked wrapper charts | controller deployments and webhooks healthy |
| `0` | namespace, quotas, service accounts, default-deny networking | foundation health and `pods: "0"` confirmed |
| `10`/`11` | native validating admission policies and bindings | policy conformance tests pass |
| `20`-`22` | held Kueue resources and suspended JobSet API canary | CRDs established; queues remain held at zero |

The chart applications override their wrapper defaults to use cert-manager objects. The rendered
Helm manifests therefore contain no repository-authored TLS private-key `Secret`. cert-manager
creates and rotates runtime TLS material after activation.

## Argo CD and cert-manager bootstrap

Argo CD is itself outside Argo CD's reconciliation boundary. The controlled live repository must
pin an exact Argo CD Helm chart version and chart artifact digest, render it with CRDs, validate
the render, install CRDs before controllers, and record the resulting evidence. This repository
does not invent that deployment-specific pin.

cert-manager is separately locked by URL, byte count, and SHA-256 in
`argocd/bootstrap/argocd.lock.yaml`. Verify those bytes before installing its CRDs and controller;
then wait for the webhook to become healthy before activating either operator application. The
operator wrapper charts and their vendored dependencies are locked by their `Chart.lock` files,
and controller images are digest-pinned in their values.

## Offline validation

Run from the repository root in the pinned tool environment:

```bash
nix develop .#ci --command bash infra/gitops/tests/validate.sh
tools/dev/bazelw test //infra/gitops:validate --test_output=errors
```

The validator invokes no cluster API and does not fetch locked release artifacts. It renders every
selectable GitOps root, checks the paused/exact-revision contract, validates all environment
Kustomize roots, and renders the local operator charts with cert-manager enabled to prove they do
not author `Secret` objects. The pinned tool environment owns kubeconform schema availability.
