# Training / Distributed

**Status:** bounded DDP reference subset implemented; qualification and production activation
remain separate evidence gates.

This package owns one-process-per-device PyTorch DDP for two closed topologies:

- one local node, two CPU ranks, Gloo; and
- one local node, eight CUDA ranks, NCCL (the declared H100 qualification target).

`TorchrunEnvironment` accepts launcher-provided `RANK`, `LOCAL_RANK`, `WORLD_SIZE`, and
`LOCAL_WORLD_SIZE`; it never creates rank assignments. Multi-node layouts are rejected.
`distributed_session` sets the CUDA device before process-group initialization, validates the
active group, and destroys the group in `finally` without relying on a final barrier. The local
world-size-one CPU/CUDA path does not initialize a process group.

`wrap_ddp` requires explicitly placed float32 model state. `DDPReducer` requires the active default
group, checks that every rank has the same microbatch/accumulation schedule, reduces integer
denominator/sample/microbatch totals, and reduces detached loss numerators. The trainer multiplies
each local loss numerator by world size before backward to compensate for DDP's average-gradient
semantics. Rank-local cancellation is reduced at matching safe points.

`shard_supervised_batch` is a deterministic, disjoint, strided fixture for bounded qualification
batches. It exposes stable global sample IDs and a global cursor; it is not a streaming loader,
worker sampler, shuffle policy, or general data-pipeline claim.

## Failure and ownership limits

- Every rank must call trainer and checkpoint operations in the same order. A rank-local Python,
  CUDA, or model exception between safe points can leave peers waiting until the configured
  process-group timeout or launcher termination; in-process recovery is not promised.
- A failed collective or optimizer transaction invalidates the live distributed objects. Tear down
  the group and restore a verified checkpoint into fresh model, optimizer, trainer, and DDP objects.
- Gloo and NCCL availability is checked at initialization. CUDA/NCCL/H100 correctness and
  performance still require the connected eight-GPU qualification lane.
- The runtime does not attach to caller-owned process groups, retry rendezvous, or perform elastic
  membership changes.

FSDP, sharded-state resharding, tensor/pipeline/expert parallelism, multi-node/RDMA, mixed precision,
compiled DDP, Spot/preemption recovery, H200/B200, and arbitrary rank counts remain unimplemented or
unqualified. Deployable composition and canonical checkpoint commit publication belong to services
and protocol owners rather than this package.
