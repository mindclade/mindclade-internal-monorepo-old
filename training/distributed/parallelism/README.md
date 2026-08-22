# Training / Distributed / Parallelism

**Status:** replicated `DistributedDataParallel` wrapping implemented for the bounded reference
topology; all other parallelism families remain scaffolded.

`wrap_ddp` accepts a validated active `DistributedContext` and a model already placed on that
context's single CPU or CUDA device. It rejects pre-wrapped, empty, frozen-only, mixed-device, and
non-float32 floating state. CPU/Gloo uses no device IDs; CUDA/NCCL binds exactly the torchrun local
rank. Buffer synchronization is enabled and unused-parameter discovery is disabled for the bounded
reference path.

The implemented topologies are one-node CPU/Gloo world 2 and one-node CUDA/NCCL world 8. FSDP,
resharding, tensor/pipeline/context/expert/sequence parallelism, device meshes, compiled DDP,
multi-node layouts, and elastic membership remain unimplemented or unqualified. The two-rank Gloo
path is tested by `//training/distributed/tests:test_distributed_smoke`; the NCCL/H100 path still
requires connected eight-GPU qualification evidence.
