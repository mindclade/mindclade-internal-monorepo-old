# Kubernetes foundation runbook

This runbook covers the fail-closed `mindclade-system` foundation. It does not
authorize direct production mutation. Prefer repository changes and the
approved GitOps controller when GitOps owns the target.

## 1. Select and render an environment

Use an explicit environment; never derive a target from the current context:

```bash
KUBE_ENV=development
KUBE_ROOT="infra/kubernetes/overlays/${KUBE_ENV}"
test -f "${KUBE_ROOT}/kustomization.yaml"
kustomize build "${KUBE_ROOT}"
```

Expected foundation output contains Namespace, ConfigMap, two ServiceAccounts,
ResourceQuota, LimitRange, two PriorityClasses, two NetworkPolicies, and the
native admission policies/bindings. It contains no Pod-producing workload,
RBAC grant, Secret, LoadBalancer, NodePort, PVC, or custom resource.

Run the complete offline gate before consulting a cluster:

```bash
nix develop .#ci --command bash infra/kubernetes/tests/validate.sh
```

It renders every declared root and chart, checks locks, schemas, object scope,
and policy against the same repository bytes. Do not validate one root and
deploy another.

## 2. Identify a cluster before any cluster command

Record the intended account/project, cluster, context, environment, and
namespace in the change ticket. Then verify rather than switch implicitly:

```bash
KUBE_CONTEXT="approved-context-name"
kubectl config current-context
kubectl --context "${KUBE_CONTEXT}" get namespace mindclade-system
kubectl --context "${KUBE_CONTEXT}" get resourcequota,limitrange,networkpolicy,serviceaccount \
  -n mindclade-system
```

Stop if the current and approved contexts differ, the namespace environment
annotation differs from `KUBE_ENV`, or a GitOps controller is not the expected
field manager.

For an approved read-only comparison:

```bash
kubectl --context "${KUBE_CONTEXT}" diff --server-side -k "${KUBE_ROOT}"
```

A server-side diff can contact admission webhooks and exposes intended changes,
but it is not rollout evidence. Do not run `kubectl apply`, delete, scale,
context changes, CRD installation, or `helm upgrade --install` without explicit
approval.

## 3. Expected fail-closed behavior

- Workload creation is rejected because `pods` and workload object counts are
  zero.
- LoadBalancer, NodePort, PVC, and GPU requests are rejected by quota.
- Pods have no ingress or egress except cluster DNS after activation.
- A Pod that omits `serviceAccountName` receives the default ServiceAccount;
  suspended templates use `mindclade-workload`. Neither receives an API token
  or RBAC grant.
- Provider-specific RuntimeClasses are absent rather than guessed.

If any workload is running in this namespace before activation, treat it as
drift. Preserve evidence, identify its owner/field manager, and do not delete it
until impact and recovery are understood.

## 4. Activation procedure

Activation is a release change, not an operator command.

1. Complete every gate in `PRODUCTION_READINESS.md` for the named deployable.
2. Add its manifests, identity, policies, digest, probes, resources, SLO, and
   rollback together.
3. Raise only the relevant object/resource quota in the target overlay; retain
   unrelated zero ceilings.
4. Change the namespace and ConfigMap activation markers in that same review.
5. Render, schema-validate, policy-test, and inspect the complete object list.
6. Obtain a server-side diff against the isolated non-production target.
7. Reconcile through GitOps, observe admission and rollout, then exercise
   readiness, drain, disruption, and rollback.
8. Promote the identical digest through staging before production.

Never put a secret value in a patch. Reference the approved secret integration
and verify only metadata and access, not secret contents.

## 5. Diagnosis

Gather read-only evidence first:

```bash
kubectl --context "${KUBE_CONTEXT}" get deploy,sts,ds,job,cronjob,svc,pod \
  -n mindclade-system -o wide
kubectl --context "${KUBE_CONTEXT}" get events -n mindclade-system \
  --sort-by=.lastTimestamp
kubectl --context "${KUBE_CONTEXT}" describe resourcequota mindclade-system \
  -n mindclade-system
```

Classify failures before changing manifests:

| Symptom | First checks |
|---|---|
| Forbidden quota | Requested object kind/resource versus overlay hard limits |
| Pod Security rejection | Restricted-profile violation and exact container field |
| Pending Pod | Requests, taints/tolerations, affinity, quota, PVC, GPU capacity, Kueue admission |
| ImagePullBackOff | Exact digest, registry IAM/workload identity, admission result |
| CrashLoopBackOff | Exit code, configuration references, startup probe, writable paths |
| Unready rollout | Readiness dependency, policy freshness, warmup, endpoints, drain state |
| DNS/network timeout | DNS policy selector, destination allowlist, port/protocol, Dataplane V2 flow logs |
| RBAC denial | ServiceAccount and exact verb/apiGroup/resource/subresource; never add wildcard access |

Read logs only for the affected workload and use bounded tails. Do not `exec`
into a production Pod by default; if it is necessary, document what will be
read and why the action cannot mutate application state.

## 6. Rollback and containment

- Stop a rollout through the owning GitOps change or controller; do not create
  a competing direct field manager.
- Roll back to the last qualified image and manifest digest, then verify
  readiness, error rate, saturation, and drift.
- Lowering quota does not terminate existing Pods. Scale/drain workloads first,
  verify zero use, then restore the blocked quota and activation markers.
- Network or identity incidents should be contained with a reviewed narrow
  policy/credential revocation; preserve audit and rendered-manifest evidence.
- Never remove finalizers, delete CRDs, rotate credentials, or bypass admission
  as an improvised rollback.

After recovery, retain events, rollout status, rendered digest, image digest,
alert timeline, and the final empty GitOps diff with the incident record.
