# Kubernetes foundation runbook

This runbook covers the fail-closed system and capacity foundations. It does
not authorize direct production mutation. Prefer repository changes and the
approved GitOps controller when GitOps owns the target.

## 1. Select and render an environment

Use an explicit environment; never derive a target from the current context:

```bash
KUBE_ENV=development
KUBE_ROOT="infra/kubernetes/overlays/${KUBE_ENV}"
test -f "${KUBE_ROOT}/kustomization.yaml"
kustomize build "${KUBE_ROOT}"
```

Expected foundation output contains the system namespace and the isolated CPU,
H100, and B200 capacity namespaces; their identities, zero quotas,
workload-specific LimitRanges, default-deny policies, PriorityClasses, and the
native admission policies/bindings. It contains no active Pod, RBAC grant,
Secret, LoadBalancer, NodePort, or PVC.

Run the complete offline gate before consulting a cluster:

```bash
nix develop .#ci --command tools/dev/bazelw test \
  //infra/kubernetes:validate --test_output=errors
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
for namespace in mindclade-system mindclade-batch-cpu mindclade-training-h100 mindclade-training-b200; do
  kubectl --context "${KUBE_CONTEXT}" get resourcequota,limitrange,networkpolicy,serviceaccount \
    -n "${namespace}"
done
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
- CPU, H100, and B200 queues are independently held with zero nominal CPU,
  memory, ephemeral-storage, Pod, and (where applicable) GPU quota.
- LoadBalancer, NodePort, PVC, and GPU requests are rejected by quota.
- Capacity workloads have no inferred DNS or dependency egress. An activation
  bundle must add the exact required flows.
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
3. Attach non-placeholder activation, release, and capacity evidence digests;
   record the minimum guaranteed node snapshot and rollout-peak calculation.
4. Raise only the relevant ResourceQuota and matching Kueue resource vector;
   retain unrelated zero ceilings and keep borrowing/preemption disabled.
5. Change that namespace to `workload-activation=active` and
   `kueue-enabled=true`, then release only its matching ClusterQueue and
   LocalQueue holds in the same review.
6. Render, schema-validate, policy-test, and inspect the complete object list.
7. Obtain a server-side diff against the isolated non-production target.
8. Reconcile through GitOps, observe admission and rollout, then exercise
   readiness, drain, disruption, and rollback.
9. Promote the identical digest through staging before production.

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

## 6. Operator transaction

Operator installation is a platform change, never a workload-side dependency.
Keep all Applications paused until a qualification window, then advance only
after the previous phase is Healthy:

```text
cert-manager CRDs
cert-manager controller and webhooks
JobSet CRDs
JobSet controller and webhooks
Kueue CRDs
Kueue controller and webhooks
GMP operator monitors and recording rules
held ResourceFlavors, Topology, ClusterQueues, and LocalQueues
```

CRD phases use server-side apply and never prune. Stop if API discovery does
not show the expected served/storage versions, a webhook is unavailable, a CRD
has an unexpected stored version, or the live release identity is an older
`mindclade-*` name. Renaming an installed Helm release requires an explicit
adoption/migration plan.

The base operator namespaces deny all ingress and egress. Before unpausing a
controller, a live overlay must provide only the observed API-server, DNS,
kubelet probe, webhook, and GMP collector flows. Do not add a broad operator
network exception to make an installation succeed.

## 7. Rollback and containment

- Stop a rollout through the owning GitOps change or controller; do not create
  a competing direct field manager.
- Roll back to the last qualified image and manifest digest, then verify
  readiness, error rate, saturation, and drift.
- Lowering quota does not terminate existing Pods. Scale/drain workloads first,
  hold the matching queues, verify zero use, then restore zero quota and the
  blocked activation markers.
- Network or identity incidents should be contained with a reviewed narrow
  policy/credential revocation; preserve audit and rendered-manifest evidence.
- Never remove finalizers, delete CRDs, rotate credentials, or bypass admission
  as an improvised rollback.

After recovery, retain events, rollout status, rendered digest, image digest,
alert timeline, and the final empty GitOps diff with the incident record.
