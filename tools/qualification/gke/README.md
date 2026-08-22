# GKE hardware release qualification

Two fail-closed surfaces live here and have deliberately different hardware inventories:

- `//tools/qualification/gke:run` is the existing all-at-once H100/H200 product-capacity
  runner backed by this directory's suspended one-GPU templates.
- `//tools/qualification/gke:foundation_image` is the source-owned CPU/H100/B200 probe for the
  isolated, eight-GPU foundation package under `infra/kubernetes/platform/qualification`.

They are not interchangeable. The foundation package follows the current H100 A3 Mega and B200
A4 High platform contract; the product-capacity runner retains its separately reviewed H200
surface until a versioned migration changes it.

## Product-capacity runner

This package owns the live, fail-closed H100 and H200 qualification runner.
The checked-in Jobs are suspended, use a zero digest under `registry.invalid`,
request exactly one GPU, and cannot be applied as runnable release work.

`//tools/qualification/gke:run --validate-only` checks both templates without
cluster access. A live run requires all of the following and qualifies both
profiles; there is no single-profile release mode:

- an explicit kubectl context matching the current context;
- Kubernetes credentials authorized to create Jobs and inspect pods/nodes;
- active capacity-contract labels, an unheld Kueue ClusterQueue with nonzero
  GPU quota, and a Ready node with the exact H100/H200 profile label;
- a release qualification image pinned by a nonzero SHA-256 digest;
- a bounded run ID and absolute evidence output path.

```bash
tools/dev/bazelw run //tools/qualification/gke:run --config=ci -- \
  --context "$MINDCLADE_GKE_CLUSTER_CONTEXT" \
  --image "$MINDCLADE_QUALIFICATION_IMAGE" \
  --run-id "$MINDCLADE_QUALIFICATION_RUN_ID" \
  --output /absolute/evidence/gke-hardware.json
```

Each image must contain `/opt/mindclade/bin/release-hardware-qualification`.
The command emits exactly one complete JSON record; missing, nonpositive, or
wrong-profile metrics fail the release. Jobs retain their normal 24-hour TTL so
cluster logs/status remain inspectable after a failed release.

## Foundation probe image

The recovered foundation probe closes the source/image gap documented by the dedicated
qualification namespace. It performs deterministic CPU-runtime, scratch-storage integrity, and
Unix-domain-socket checks and emits one provenance-bearing JSON result. H100 and B200 additionally
require exactly eight matching GPUs plus a separately reviewed
`/opt/mindclade/bin/release-gpu-qualification` helper that proves CUDA device count, a minimum
1-GiB NCCL all-reduce, positive measured bus bandwidth, at least 1 GiB of tested GPU memory, and
passing DCGM health. The base image deliberately omits that helper, so GPU qualification fails
closed instead of claiming partial evidence.

The checked-in `foundation_push` target points only at localhost and is not in the release target
catalog. A separate reviewed change must bind an immutable destination, ARC release authority,
connected qualification evidence, and a ready GitOps receiver before publication is possible.

Run the source-only tests without credentials:

```bash
tools/dev/bazelw test //tools/qualification/gke:tests --config=ci
tools/dev/bazelw build //tools/qualification/gke:foundation_image --config=ci
```
