# Kueue module

This module owns Mindclade's queue policy, not the controller lifecycle. The controller and
CRDs are installed from the locked wrapper chart in `chart/`; `resources.yaml` is reconciled
only after that chart is healthy.

The wrapper uses the separately locked cert-manager module for webhook certificates. Install
and prove cert-manager ready before this chart; no certificate key material is rendered here.

Current state is deliberately blocked. The `mindclade-batch-cpu`,
`mindclade-training-h100`, and `mindclade-training-b200` ClusterQueue/LocalQueue pairs are held
and every nominal quota is zero. Their namespaces also declare `kueue-enabled=false`; native
admission maps each namespace to exactly one queue and workload class.

The H100 and B200 ResourceFlavors reference `mindclade-gpu-zone-host`. The H100 flavor also
requires the Terraform-owned `mindclade.dev/capacity-type=on-demand` node label. `1g-packed` Jobs
use unconstrained topology so Kueue can fill partially occupied eight-GPU nodes. The H100
qualification JobSet consumes one complete eight-GPU node; the unqualified B200 template retains
the older two-Pod, same-zone shape. CPU, memory, ephemeral storage, Pod count, and GPU are
deliberately in the same GPU flavor group so Kueue cannot combine incompatible CPU-node and
GPU-node selectors.

The CPU queue uses only the Terraform-guaranteed `general-purpose` / `on-demand` CPU node label
tuple. High-memory and Spot capacity require separate measured flavors and queue policy; the
foundation does not infer tolerations for either taint.

## Controller install

```bash
helm lint infra/kubernetes/platform/kueue/chart
helm template kueue infra/kubernetes/platform/kueue/chart \
  --namespace kueue-system --include-crds
```

The wrapper locks Kueue `0.19.1`, vendors the dependency archive, and pins the controller image
digest in `values.yaml`. `versions.env` records both the upstream OCI artifact digest and the
vendored archive digest; `Chart.lock` locks the dependency graph. The controller opts into only
Kubernetes Jobs and JobSet and only in explicitly labeled namespaces. Controller CRDs must be
upgraded before these custom resources. Its CPU, memory, and ephemeral-storage requests and limits
are explicit.

The Kueue controller requires a Kubernetes API token to reconcile workloads, queues, Jobs, and
JobSets. Its namespace therefore uses the explicit `platform-operator` admission class instead of
the standard workload token prohibition. Before activation, audit the rendered ServiceAccount and
exact upstream RBAC rules, token rotation/audience, observed API calls, and default-deny network
exceptions. This does not authorize user workloads in the operator namespace.

Install CRDs with `kueue.controller.enabled=false,kueue.crds.enabled=true`, wait for
`Established`, then install the controller with the inverse flags. Never remove Kueue CRDs during rollback: first hold
queues, drain or finish admitted work, roll back the controller to a CRD-compatible version,
and preserve all custom resources.

Do not run `helm dependency update` as an ordinary render step: upstream `0.19.1` has the
cluster-scope defect documented in `chart/patches/README.md`, and a raw refresh overwrites the
reviewed vendored patch. Validation checks the vendored digest and the rendered object scopes.

## Activation review

Before changing the held defaults, record measured cluster allocatable capacity after system
reservation, per-tenant quotas, preemption policy, checkpoint behavior, queue wait-time SLOs,
and alert ownership. Publish those records as immutable objects and place their non-zero SHA-256
digests in the namespace capacity-evidence, release-evidence, and activation-bundle annotations.
Create the corresponding immutable, digest-addressed capacity-contract ConfigMap and reference
its exact name from the Namespace; the base schema ConfigMap has no capacity values and cannot be
used for activation.
Then change all of the following in one reviewed GitOps promotion:

1. set non-zero nominal quota no larger than measured allocatable capacity;
2. atomically change only the approved namespace to `workload-activation=active` and
   `kueue-enabled=true`, retaining its exact `mindclade.dev/workload-class` mapping;
3. change the selected LocalQueue and ClusterQueue `stopPolicy` to `None`;
4. use only digest-pinned, qualified Job or JobSet templates and replace zero ResourceQuota
   ceilings with measured values no larger than the selected ClusterQueue quota.

Rollback is `Hold` for graceful stop or `HoldAndDrain` for an incident. Quota increases and
preemption changes require a fresh capacity and failure-domain review.
