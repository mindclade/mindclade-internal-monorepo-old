# GKE hardware release qualification

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
