# Bazel remote execution foundation

This module creates the Google Cloud foundation for a private Bazel remote execution
service on an existing regional GKE Standard cluster:

- a protected, autoscaled general-purpose or high-memory CPU node pool through the
  canonical `cpu_node_pool` module;
- a dedicated protected node identity with the narrow GKE node role;
- a separate protected executor workload identity, bound keylessly to one Kubernetes
  service account;
- additive Artifact Registry/project IAM and append-only read/create access to an
  existing `bazel_remote_cache` bucket;
- an immutable executor image contract and the node selector/toleration values GitOps
  must deploy.

```hcl
module "bazel_remote_execution" {
  source = "../../modules/bazel_remote_execution"

  project_id               = "mindclade-build"
  cluster_name             = "mindclade-platform"
  region                   = "us-central1"
  node_locations           = ["us-central1-a", "us-central1-b"]
  pod_secondary_range_name = "gke-pods"
  node_service_account_id  = "bazel-executor-nodes"
  executor_image           = "us-central1-docker.pkg.dev/mindclade-build/workers/bazel@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  cache_bucket_name        = "mindclade-bazel-cas"
  environment              = "production"
  owner                    = "release-engineering"
}
```

## Boundary

Terraform does **not** deploy the remote-execution server, Kubernetes namespace,
KSA, NetworkPolicy, PodDisruptionBudget, autoscaler driven by execution backlog,
certificates, load balancer, Bazel platform, Nix worker closure, or endpoint. Those
belong to GitOps, Bazel, and Nix. `gitops_contract` is the exact handoff; a green
Terraform plan is not a claim that remote execution works.

The remote cache bucket is created by `bazel_remote_cache`, never here. The executor
can read and create objects but cannot administer the bucket or its lifecycle. CAS,
Nix binary cache, platform artifacts, and audit evidence stay separate because their
mutation, retention, and recovery policies differ.

SPOT capacity is opt-in, requires the exact acknowledgement, and must scale to zero.
Callers must prove interruption retry, deterministic outputs, bounded cancellation,
cache miss/corruption behavior, multi-zone capacity, private connectivity, image
attestation, and cold/warm performance before enabling it. No service-account key is
created or accepted.

## Upgrade and rollback

Roll out a new immutable executor image through GitOps, canary remote execution, then
change the default platform. Keep the prior digest and node pool until result parity,
cache compatibility, cancellation, and performance pass. Rollback selects the prior
digest or local execution; do not delete protected identities or the cache during an
incident. Node-pool replacement is a reviewed capacity migration, not an ordinary
in-place edit.
