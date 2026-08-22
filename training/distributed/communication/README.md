# Training / Distributed / Communication

**Status:** exact-count DDP loss reduction implemented for the bounded reference topology;
general collective, hook, transport, metric, and diagnostic surfaces remain scaffolded.

`DDPReducer` implements the reduction contract owned by `training/core`. It requires the active
default process group to match a validated `DistributedContext`, checks the rank schedule before
training, sums integer denominators/sample/microbatch counts, sums detached loss numerators in
float64, and reduces cancellation with boolean-any semantics. Its backward scale is exactly the
world size, compensating for `DistributedDataParallel` gradient averaging so the trainer computes
the gradient of the global loss sum divided by the exact global denominator.

The supported process groups are the closed topologies documented in the parent package: one-node
CPU/Gloo world 2 and one-node CUDA/NCCL world 8. This module does not create groups, move tensors,
recover failed collectives, provide custom communication hooks, or claim arbitrary metric
reduction. Every rank must enter operations in the same order; rank-local failures can leave peers
waiting for the configured group timeout or launcher termination.

The two-rank numerical and failure-path coverage is owned by
`//training/distributed/tests:test_distributed_smoke`. The NCCL/H100 path still requires connected
eight-GPU qualification evidence before activation.
