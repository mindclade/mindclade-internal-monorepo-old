# GitOps bootstrap and recovery runbook

This runbook describes the controlled operator procedure. It does not authorize a live change.
Use the live repository's change window, approval, identity, and evidence process. Never run a
mutation before confirming the current context, destination, diff, and rollback commit.

## 1. Validate the source offline

From the repository root in the pinned tool environment:

```bash
infra/gitops/tests/validate.sh
```

For the selected environment, also preserve deterministic render evidence:

```bash
selected_environment=development
kustomize build --load-restrictor LoadRestrictionsRootOnly \
  "infra/gitops/bootstrap/${selected_environment}" > /tmp/mindclade-gitops-bootstrap.yaml
kustomize build --load-restrictor LoadRestrictionsRootOnly \
  "infra/gitops/environments/${selected_environment}" > /tmp/mindclade-foundation.yaml
shasum -a 256 /tmp/mindclade-gitops-bootstrap.yaml /tmp/mindclade-foundation.yaml
```

Use an explicit selected environment; do not build or concatenate all three bootstrap roots.

## 2. Verify locked upstream bytes

The offline validator intentionally does not fetch release artifacts. In the controlled connected
bootstrap job, fetch cert-manager to a fresh temporary file and compare both properties in
`argocd/bootstrap/argocd.lock.yaml` before rendering or applying it:

```bash
lock_file=infra/gitops/argocd/bootstrap/argocd.lock.yaml
locked_url="$(yq eval -r '.data."cert-manager.url"' "${lock_file}")"
locked_sha="$(yq eval -r '.data."cert-manager.sha256"' "${lock_file}")"
locked_bytes="$(yq eval -r '.data."cert-manager.bytes"' "${lock_file}")"
download_file="$(mktemp "${TMPDIR:-/tmp}/mindclade-cert-manager.XXXXXX")"
trap 'rm -f -- "${download_file}"' EXIT
curl --fail --silent --show-error --location --output "${download_file}" "${locked_url}"
test "$(wc -c < "${download_file}" | tr -d ' ')" = "${locked_bytes}"
test "$(shasum -a 256 "${download_file}" | awk '{print $1}')" = "${locked_sha}"
```

Any mismatch stops the release. Update a lock only in a reviewed dependency promotion; never
accept new bytes merely to clear the failure.

## 3. Bootstrap controllers

1. In the controlled live repository, pin the Argo CD Helm chart by exact version and downloaded
   artifact digest. Verify provenance, render with CRDs, validate the output, and archive its
   digest. This source repository intentionally has no guessed Argo CD pin.
2. Confirm the target context and effective authorization using read-only commands. Install Argo
   CD CRDs before its controllers, then wait for API discovery and controller health.
3. Diff and apply `infra/gitops/argocd/projects.yaml` as the separately approved governance
   layer. The root application is not permitted to manage these projects.
4. Register `https://github.com/mindclade-org/mindclade.git` through the live secret system. Do
   not commit or print repository credentials. Prove an unauthorized identity and mutable source
   revision are denied.
5. Install the verified cert-manager bytes with CRDs before controllers. Wait for its webhook and
   API discovery before proceeding.

Before a live diff or apply, record at minimum:

```bash
kubectl config current-context
kubectl config view --minify
kubectl auth can-i create applications.argoproj.io --namespace argocd
kubectl auth can-i create appprojects.argoproj.io --namespace argocd
```

Stop if the context, server, identity, or permissions differ from the approved change record.

## 4. Instantiate one root application

Create a live-repository Kustomize overlay for `argocd/app-of-apps.yaml` that:

- replaces `SET_ENVIRONMENT` with exactly one of `development`, `staging`, or `production`;
- replaces `SET_EXACT_40_CHAR_COMMIT_SHA` with the reviewed lowercase 40-character commit;
- removes only the root application's `skip-reconcile` annotation;
- keeps automatic sync, prune, and self-heal disabled.

Diff the live overlay, apply it during the approved window, and perform a manual root sync. The
root creates repository/content-lock ConfigMaps and paused child applications. It cannot rewrite
its project and cannot reconcile any child while their pause annotations remain.

## 5. Activate waves deliberately

Use live overlays to replace the same exact revision in every selected child. Remove pauses one
wave at a time, keep sync manual, and wait for health before continuing:

1. wave `-20`: Kueue controller, then JobSet controller. Their local Helm wrappers enable
   cert-manager and render no private-key Secret. Confirm CRDs are `Established`, webhooks have
   endpoints, deployments are available, and APIService health is green where present.
2. wave `0`: foundation. Confirm `mindclade-system` PSS/admission labels, zero Pod quota, default
   ServiceAccount hardening, default-deny networking, and environment identity before continuing.
3. waves `10`/`11`: native admission resources arrive through the foundation render. Run both
   allowed and denied server-side admission probes from reviewed fixtures.
4. waves `20`-`22`: keep paused until the ML activation review. If approved, verify every Kueue
   queue remains `Hold` with zero nominal quota and the JobSet canary remains suspended.

Do not use `--prune` during initial qualification. Never delete operator CRDs during rollback.

## 6. Pause, rollback, and recover

For a GitOps incident, first restore `skip-reconcile: "true"` on the affected application. Pausing
reconciliation does not undo existing resources. Hold Kueue queues and suspend JobSets before a
controller or manifest rollback. Then select a previously qualified exact commit, render and diff
it, and manually synchronize only the affected application. Preserve CRDs and custom resources;
roll a controller back only to a version compatible with every stored CR version.

If the root application or repository credential is compromised, pause all child applications,
revoke the external credential through its owning secret system, and use the separately managed
AppProjects to contain access. Restore Argo CD state from the qualified backup, re-register a
rotated credential without printing it, and reconcile from a reviewed exact commit.

Useful read-only diagnostics include:

```bash
argocd app get mindclade-bootstrap --refresh=false
argocd app list --output wide
kubectl get applications.argoproj.io,appprojects.argoproj.io --namespace argocd
kubectl get customresourcedefinitions.apiextensions.k8s.io
kubectl get validatingwebhookconfigurations.admissionregistration.k8s.io
```

Capture timestamps and sanitized output in the incident record. Never paste repository Secret
data, access tokens, kubeconfig contents, or rendered private keys into logs or tickets.
