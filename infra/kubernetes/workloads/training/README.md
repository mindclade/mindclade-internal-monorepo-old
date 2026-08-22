# Training workload templates

`base/` is a non-deployable Kustomize Component. Its `unbound` namespace-independent queue,
profile, workload-class, and ServiceAccount values are sentinels; never apply its rendered output.
Only these roots are deployable inputs:

- `overlays/h100` renders into `mindclade-training-h100` and selects
  `gke-h100-a3-megagpu-8g` / `mindclade-training-h100`.
- `overlays/b200` renders into `mindclade-training-b200` and selects
  `gke-b200-a4-highgpu-8g` / `mindclade-training-b200`.

Each overlay renders two permanently suspended templates. `1g-packed` requests one GPU and uses
Kueue unconstrained topology for fragmentation-aware placement. The H100 `8g` profile is the
reference qualification shape: exactly one JobSet Pod, eight GPUs, world size eight, and explicit
on-demand selection. The B200 `8g-full` reservation remains the older two-Pod, same-zone shape and
is outside the reference qualification claim. Every template retains the zero-digest image gate,
named tokenless namespace ServiceAccount, restricted Pod security, bounded resources/scratch, and
an unqualified checkpoint/transport contract.

The H100 source profile is not an activation bundle. Its queue is held, namespace quotas are zero,
and the images are the all-zero digest sentinels. Spot, H200, B200, multi-node, and live capacity
remain unsupported by this qualification profile. The overlay owns scheduling and capacity
invariants; its annotations bind each phase to the exact executable/container template under
`tools/qualification/training_gke` instead of duplicating the trainer/checkpoint-agent command
contract in two source authorities.

Activation is never an edit to these static templates. A separate reviewed bundle must provide
qualified image and evidence digests, exact network and identity contracts, measured namespace
and queue quotas, and then satisfy the native namespace activation policy.
