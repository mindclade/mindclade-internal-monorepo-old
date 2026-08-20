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
use leader election, hard container security defaults, bounded resources, topology
anti-affinity, and a PDB.

Install or upgrade the CRDs and controller before reconciling any JobSet. Validate stored
versions and conversion compatibility before changing the API version. Rollback never deletes
the CRD: suspend JobSets, allow child Jobs to checkpoint or terminate, and roll back only to a
controller compatible with the stored resources.

## Workload requirements

Production JobSets must remain suspended until Kueue admission, image provenance, checkpoint
restore, topology placement, rendezvous, numerical parity, cancellation, and retry behavior
are qualified. Multi-node GPU templates must explicitly request `nvidia.com/gpu`, tolerate the
GPU taint, select a reviewed GPU profile, and bound CPU, memory, ephemeral storage, and runtime.
