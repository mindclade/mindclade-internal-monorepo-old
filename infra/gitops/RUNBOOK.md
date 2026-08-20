# GitOps bootstrap and recovery runbook

This runbook describes a controlled operator procedure; it does not authorize a live mutation.
Use an approved change window and record context, identity, diffs, health evidence, and rollback
commit. Never log repository credentials, kubeconfig contents, access tokens, or private keys.

## 1. Validate and preserve offline evidence

From the repository root:

```bash
nix develop .#ci --command tools/dev/bazelw test \
  //infra/gitops:validate --test_output=errors

selected_environment=development
kustomize build --load-restrictor LoadRestrictionsRootOnly   "infra/gitops/bootstrap/${selected_environment}" > /tmp/mindclade-gitops-bootstrap.yaml
kustomize build --load-restrictor LoadRestrictionsRootOnly   "infra/gitops/environments/${selected_environment}" > /tmp/mindclade-foundation.yaml
sha256sum /tmp/mindclade-gitops-bootstrap.yaml /tmp/mindclade-foundation.yaml
```

Select exactly one environment. Archive these digests with the immutable commit and validator
output; do not concatenate environments.

## 2. Re-verify cert-manager provenance during promotion

The committed deployment source is a deterministic split of the cert-manager v1.19.1 static
release. The complete upstream file is temporary promotion input, not a third checked-in copy.
A connected promotion job must verify the locked bytes before parsing and reproduce the committed
artifacts exactly:

```bash
lock_file=infra/gitops/argocd/bootstrap/argocd.lock.yaml
locked_url="$(yq eval -r '.data."cert-manager.url"' "${lock_file}")"
locked_sha="$(yq eval -r '.data."cert-manager.sha256"' "${lock_file}")"
locked_bytes="$(yq eval -r '.data."cert-manager.bytes"' "${lock_file}")"
download_file="$(mktemp "${TMPDIR:-/tmp}/mindclade-cert-manager.XXXXXX")"
trap 'rm -f -- "${download_file}"' EXIT

curl --fail --silent --show-error --location --output "${download_file}" "${locked_url}"
python3 infra/gitops/vendor/cert-manager/split_release.py   --source "${download_file}"   --expected-sha256 "${locked_sha}"   --expected-bytes "${locked_bytes}"   --crds-output infra/gitops/vendor/cert-manager/v1.19.1/crds/upstream.yaml   --controllers-output infra/gitops/vendor/cert-manager/v1.19.1/controllers/upstream.yaml   --check
```

The splitter fails before parsing on a byte-count or SHA mismatch. It also fails if either
generated file differs, the phase inventories overlap, their counts differ from 6/43, or their
normalized union differs from all 49 upstream objects. Never update a lock merely to clear a
failure.

For JobSet and Kueue promotions, verify the upstream OCI and archive locks, run
`infra/kubernetes/platform/operator-system/repack_chart.py`, reproduce the vendored digest, and
prove phase/full parity. Do not run `helm dependency update` as a release shortcut.

## 3. Prepare Argo CD and governance

1. In the controlled live repository, pin the exact Argo CD chart, archive/provenance digest,
   values, and rendered digest. Install Argo CRDs before controllers and qualify HA, backup,
   restore, and health behavior.
2. Record read-only context and authorization evidence:

   ```bash
   kubectl config current-context
   kubectl config view --minify
   kubectl auth can-i create applications.argoproj.io --namespace argocd
   kubectl auth can-i create appprojects.argoproj.io --namespace argocd
   ```

3. Diff and separately apply `infra/gitops/argocd/projects.yaml`. The root Application cannot
   widen those nine AppProjects.
4. Register the canonical repository through the external Secret system; prove denied access for
   an unauthorized identity and reject mutable source revisions.
5. Create a live overlay of `argocd/app-of-apps.yaml` that selects exactly one environment and
   exact commit, removes only the root pause, and keeps automated sync/prune/self-heal disabled.
   Manually sync the root. It creates only non-secret lock contracts and eleven paused children.

Stop if context, server, identity, source, project, or diff differs from the approved record.

## 4. Qualify operator isolation before controllers

Manually sync wave `-80` first. Confirm `cert-manager`, `jobset-system`, and `kueue-system`
use restricted PSS, `platform-operator` admission/workload labels, and default-deny ingress and
egress. They must not select the standard workload VAPs.

The checked-in policies intentionally allow no traffic. Before a controller phase is unpaused, a
live provider overlay must add exact, reviewed DNS, Kubernetes API, control-plane-to-webhook,
health-probe, and metrics-collector flows. Do not add unrestricted egress or guess control-plane
CIDRs. Verify denied unexpected flows and the required flows from the real cluster.

Audit each rendered controller ServiceAccount and RBAC rule. Controllers require Kubernetes API
tokens; verify token rotation/audience and observed API calls are no broader than the upstream
controller role. The exemption applies only to these operator identities and namespaces.

## 5. Activate one phase at a time

Sync waves are labels on sibling child Applications. Without a qualified Argo Application health
customization/progressive-sync contract, they do not wait for child health. Keep every later child
paused and perform these manual transactions:

1. `-70` cert-manager CRDs: diff with server-side apply, sync, wait for all six CRDs to be
   `Established`, and record stored versions. Never prune.
2. `-60` cert-manager controller: sync the digest-pinned static overlay; verify all three
   deployments have two available replicas, PDBs, bounded ephemeral storage, Service endpoints,
   webhook TLS, and CA injection.
3. `-50` JobSet CRD: sync only the CRD phase, wait for `jobsets.jobset.x-k8s.io` to be
   `Established`, and confirm controller objects are absent.
4. `-40` JobSet controller: verify the canonical release name is `jobset`, then confirm
   deployment availability, Certificates, and both webhook configurations/endpoints.
5. `-30` Kueue CRDs: sync only the eleven CRDs, wait for `Established`, and verify conversion
   CA bundles after cert-manager injection.
6. `-20` Kueue controller: verify the canonical release name is `kueue`, then confirm the
   deployment, webhooks, and both visibility APIServices are healthy.
7. `-10` operator observability: confirm the monitoring CRDs exist; qualify the exact collector
   identity, rotating TokenRequest bearer Secrets, CA-only trust Secrets, JobSet client certificate,
   Secret-read and `*/metrics` reader bindings, and collector NetworkPolicy flow. Then verify the
   PodMonitoring configuration succeeds, every expected target is healthy, and Rules evaluate
   without errors. The GitOps project does not own those external credentials or bindings.
8. `0` workload foundation: confirm environment namespaces, restricted PSS, default Service
   Account hardening, resource policy, and blocked workload activation.
9. Keep `20` and `22` paused until ML capacity approval. On an approved test, queues must still
   be held at zero and the JobSet canary must remain suspended.

After every step, capture the Argo diff/status, object inventory, controller logs/events, webhook
or API discovery evidence, alerts, and a timestamp. A healthy wave annotation is not evidence.
Do not use `Force`, `Replace`, `--prune`, or a missing-CRD dry-run bypass.

## 6. Existing-release adoption gate

Before either Helm phase, inventory labels/annotations for every rendered JobSet and Kueue object.
If an existing `mindclade-*` or other Helm release owns them, stop. Do not change `releaseName`
or overwrite ownership metadata. Approve a separate migration covering resource mapping, Helm
release state, controller downtime, rollback, CRD compatibility, and proof that only one
controller is active.

## 7. Pause, roll back, and recover

At the first failed gate, restore the affected and all downstream
`argocd.argoproj.io/skip-reconcile: "true"` annotations. Pausing does not undo applied objects.
Hold queues and suspend JobSets before controller rollback.

Roll back in reverse dependency order: observability/ML resources, Kueue controller, JobSet
controller, then cert-manager controller. Use only a previously qualified exact commit and a
controller version compatible with every stored CR version. CRD Applications remain paused and
are never pruned or deleted; their resource annotations are a guardrail, not authorization for a
manual deletion. Retain operator namespaces while any controller or webhook object exists.

If the root or repository credential is compromised, pause all children, revoke/rotate the
external credential, use separately managed AppProjects for containment, restore qualified Argo
state, and reconcile from a reviewed exact commit. Capture sanitized diagnostics:

```bash
argocd app list --output wide
kubectl get applications.argoproj.io,appprojects.argoproj.io --namespace argocd
kubectl get customresourcedefinitions.apiextensions.k8s.io
kubectl get mutatingwebhookconfigurations,validatingwebhookconfigurations
kubectl get apiservices.apiregistration.k8s.io
```
