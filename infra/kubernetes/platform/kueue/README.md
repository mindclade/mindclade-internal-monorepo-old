# Kueue module

This module owns Mindclade's queue policy, not the controller lifecycle. The controller and
CRDs are installed from the locked wrapper chart in `chart/`; `resources.yaml` is reconciled
only after that chart is healthy.

Current state is deliberately blocked. `mindclade-batch` is held, every nominal quota is zero,
its LocalQueue is held, and only namespaces labeled `mindclade.dev/kueue-enabled=true` are
eligible. All four controls are reviewed independently before a workload can be admitted.

## Controller install

```bash
helm dependency build infra/kubernetes/platform/kueue/chart
helm lint infra/kubernetes/platform/kueue/chart
helm template kueue infra/kubernetes/platform/kueue/chart \
  --namespace kueue-system --include-crds
```

The wrapper locks Kueue `0.19.1`, the OCI chart digest in `Chart.lock`, and the controller image
digest in `values.yaml`. It opts into only Kubernetes Jobs and JobSet and only in explicitly
labeled namespaces. Controller CRDs must be upgraded before these custom resources. Never
remove Kueue CRDs during rollback: first hold queues, drain or finish admitted work, roll back
the controller to a CRD-compatible version, and preserve all custom resources.

## Activation review

Before changing the held defaults, record measured cluster allocatable capacity after system
reservation, per-tenant quotas, preemption policy, checkpoint behavior, queue wait-time SLOs,
and alert ownership. Then change all of the following in one reviewed GitOps promotion:

1. set non-zero nominal quota no larger than measured allocatable capacity;
2. label only the approved workload namespaces `mindclade.dev/kueue-enabled=true`;
3. change the selected LocalQueue and ClusterQueue `stopPolicy` to `None`;
4. use only digest-pinned, qualified Job or JobSet templates.

Rollback is `Hold` for graceful stop or `HoldAndDrain` for an incident. Quota increases and
preemption changes require a fresh capacity and failure-domain review.
