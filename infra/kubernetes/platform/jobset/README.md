# JobSet module

This module installs the upstream JobSet controller through a supply-chain-locked Helm wrapper
and keeps a suspended `v1alpha2` API compatibility canary under Kustomize. The canary cannot
create a child Job and its image points to `registry.invalid` as an additional safety barrier.

The wrapper uses the separately locked cert-manager module for webhook certificates. Install
and prove cert-manager ready before this chart; no certificate key material is rendered here.

## Controller install

```bash
helm lint infra/kubernetes/platform/jobset/chart
helm template jobset infra/kubernetes/platform/jobset/chart \
  --namespace jobset-system --include-crds
```

The wrapper locks JobSet `0.12.0`, vendors the dependency archive, and pins the controller
image digest in `values.yaml`. `versions.env` records both the upstream OCI artifact digest and
the vendored archive digest; `Chart.lock` locks the dependency graph. Two controller replicas
use leader election, hard container security defaults, CPU/memory/ephemeral-storage bounds, topology
anti-affinity, and a PDB.

The JobSet controller requires a Kubernetes API token to reconcile JobSets, Jobs, and webhook
state. Its namespace therefore uses the explicit `platform-operator` admission class instead of
the standard workload token prohibition. This is not a general workload exemption: before
activation, audit the rendered ServiceAccount and exact upstream RBAC rules, token rotation and
audience, observed API calls, and isolation by the operator namespace default-deny policy.

Install CRDs with `jobset.controller.enabled=false`, wait for `Established`, then install the
controller with `--skip-crds` and `jobset.controller.enabled=true`. Validate stored
versions and conversion compatibility before changing the API version. Rollback never deletes
the CRD: suspend JobSets, allow child Jobs to checkpoint or terminate, and roll back only to a
controller compatible with the stored resources.

## Workload requirements

Production JobSets must remain suspended until Kueue admission, image provenance, checkpoint
restore, topology placement, rendezvous, numerical parity, cancellation, and retry behavior
are qualified. Multi-node GPU templates must explicitly request `nvidia.com/gpu`, tolerate the
GPU taint, select a reviewed GPU profile, and bound CPU, memory, ephemeral storage, and runtime.
