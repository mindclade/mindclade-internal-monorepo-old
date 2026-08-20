# Training workload templates

`base/` is a non-deployable Kustomize Component. Its `unbound` namespace-independent queue,
profile, workload-class, and ServiceAccount values are sentinels; never apply its rendered output.
Only these roots are deployable inputs:

- `overlays/h100` renders into `mindclade-training-h100` and selects
  `gke-h100-a3-megagpu-8g` / `mindclade-training-h100`.
- `overlays/h200` renders into `mindclade-training-h200` and selects
  `gke-h200-a3-ultragpu-8g` / `mindclade-training-h200`.

Each overlay renders two permanently suspended templates. `1g-packed` requests one GPU and uses
Kueue unconstrained topology for fragmentation-aware placement. `8g-full` requests all eight
GPUs on each of two JobSet Pods, requires one zone through Kueue TAS, and spreads across hosts.
Both retain the zero-digest image gate, named tokenless namespace ServiceAccount, restricted Pod
security, bounded resources/scratch, and an unqualified checkpoint/transport contract.

Activation is never an edit to these static templates. A separate reviewed bundle must provide
qualified image and evidence digests, exact network and identity contracts, measured namespace
and queue quotas, and then satisfy the native namespace activation policy.
