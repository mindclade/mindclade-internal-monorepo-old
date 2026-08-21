# MLflow operations runbook

This runbook is read-only by default and does not authorize a live apply, Helm install, secret
read, credential rotation, database migration, or GitOps promotion.

## Render and inspect

```sh
helm lint --strict infra/kubernetes/platform/mlflow/chart
helm template mindclade infra/kubernetes/platform/mlflow/chart \
  --namespace research-mlflow
helm template mindclade infra/kubernetes/platform/mlflow/chart \
  --namespace research-mlflow \
  --values infra/kubernetes/platform/mlflow/qualification-values.yaml
```

The first command must render no Kubernetes objects. The fixture render must contain one
ServiceAccount, migration Job, Deployment, ClusterIP Service, PDB, HPA, NetworkPolicy, and
PodMonitoring; it must contain no Secret, RBAC, Namespace, PVC, CRD, or public Service.

## Pre-deployment change sequence

1. Freeze endpoint/workspace administration for the upgrade window and record current schema,
   server version, image/render digests, workspace inventory, and effective archival policies.
2. Verify fresh SQL PITR/snapshot and GCS restore evidence. Confirm the rollback server remains
   compatible with the post-migration schema; otherwise rollback is a database restore, not an
   image-only change.
3. Build and qualify `//services/mlflow:image` for Linux/amd64. Attach SBOM, provenance,
   signature, vulnerability, dependency-import, and disposable-stack results.
4. Create/update only external secret metadata through its owner. Confirm all five required keys
   exist without reading or printing their values.
5. Produce environment values with observed CIDRs, GSA, GCS prefixes, hostname, release evidence,
   and the selected image digest. Run the complete Kubernetes validation.
6. Release the monorepo, add the exact release record/selection, activate the Helm target in
   GitOps, and review generated output. Obtain a server-side diff in isolated staging.
7. Reconcile through Argo CD. The PreSync migration Job must succeed before Deployment rollout.
   Stop on retry, timeout, unexpected DDL, connection saturation, or a mismatched evidence digest.
8. Verify `/version`, `/health`, `/metrics`, login, explicit workspace membership, cross-workspace
   denial, run/metric writes, GCS proxy upload/download, trace archival, AI Gateway invocation,
   Redis-backed rejecting budget, and model-source rejection.
9. Observe SLOs through the full canary window, perform a controlled pod disruption and rollback
   rehearsal, then record an empty GitOps diff.

## Read-only diagnosis

Use an approved explicit context and bounded output:

```sh
KUBE_CONTEXT=approved-context
kubectl --context "${KUBE_CONTEXT}" -n research-mlflow get \
  deploy,job,pod,svc,hpa,pdb,networkpolicy,podmonitoring -o wide
kubectl --context "${KUBE_CONTEXT}" -n research-mlflow get events \
  --sort-by=.lastTimestamp
kubectl --context "${KUBE_CONTEXT}" -n research-mlflow logs \
  deploy/mindclade-mlflow --all-containers --tail=200
```

Never print Secret data or a backend/Redis URI. Correlate by request/trace/run/digest identifiers;
redact payloads and credentials before attaching evidence.

| Symptom | First checks | Safe containment |
|---|---|---|
| PreSync Job failed | exit code, SQL reachability/TLS, schema version, connection budget | stop sync; do not start old/new servers concurrently against an unknown schema |
| Pods fail startup | `/version`, auth/trace config parse, required Python imports, writable `/tmp` | hold rollout and use last qualified image if schema compatible |
| Pods unready | `/health`, SQL/Redis/GCS reachability, NetworkPolicy, pool exhaustion | stop rollout; preserve events and dependency metrics |
| 401/403 | hostname/proxy identity, explicit workspace role, `grant_default_workspace_access` | fix narrow role assignment; never broaden default permission |
| Artifact failure | proxy URI, GSA binding, GCS permission/retention, storage errors | pause artifact-producing workflows; Mindclade CAS remains authoritative |
| Cross-workspace visibility | active workspace header/context and role grants | treat as security incident; freeze administration and revoke the narrow grant |
| Gateway overspend | Redis health, budget policy scope/action, refresh lag | disable affected MLflow endpoint through reviewed admin action; do not bypass budgets |
| Trace DB growth | archival config freshness, scheduler lag/errors, finalized traces | reduce ingestion or approved retention; never delete unverified evidence ad hoc |
| High latency/errors | SQL pool/saturation, Redis/GCS errors, HPA, upstream provider | shed optional mirror traffic; authoritative training/serving must continue |

## Rollback

- Before schema migration, revert the GitOps selection to the last qualified image/render.
- After a compatible expand-only migration, revert only after the older binary's schema
  compatibility test passes.
- After an incompatible migration, stop writers, restore SQL to the recorded point, restore any
  required GCS objects, then reconcile the prior image/render. Do not improvise reverse DDL.
- If credential compromise is suspected, contain access through the secret/IAM owner, preserve
  audit evidence, and rotate credentials only under the incident plan.
- If MLflow is unavailable, disable the optional mirror exporters and Gateway-dependent clients.
  Mindclade scheduling, CAS, release authority, and Rust serving must not depend on MLflow health.

Recovery is complete only after workspace negative tests, artifact/trace reads, budget rejection,
SLO recovery, migration state, and an empty GitOps diff are recorded against the recovered digest.
