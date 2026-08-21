---
name: pytorch-distributed-training
description: Implement or debug PyTorch multi-process and multi-device training with torchrun, DistributedDataParallel, FSDP, DTensor, tensor parallelism, distributed samplers, collectives, metric reduction, and distributed checkpointing. Use for scaling a trainer, rank hangs, incorrect sharding, duplicated data, or multi-rank resume. Do not use for a single-process loop unless distributed behavior is part of the task.
license: MIT
compatibility: Designed for Codex and other Agent Skills-compatible clients. Project commands require Python and the repository's installed PyTorch version.
metadata:
  version: "1.0.0"
  domain: "pytorch"
---
# Objective

Make distributed behavior explicit and correct on a two-process smoke test before scaling to the target cluster, while preserving single-process model semantics.

# Workflow

1. Inspect the launcher, cluster environment, model size, optimizer state, data source, checkpoint format, and target hardware. Detect the installed PyTorch version and available distributed backends.
2. Choose the simplest strategy that satisfies the requirement. Prefer one-process-per-device DDP for ordinary data parallelism. Use FSDP when parameter, gradient, or optimizer state memory requires sharding. Use DTensor or tensor parallelism only when model partitioning is justified.
3. Launch with `torchrun` or the repository's supported elastic launcher. Read rank, local rank, and world size from the environment; do not invent rank assignments in application code.
4. Set the local device before constructing or wrapping accelerator state. Initialize the process group with a backend compatible with the actual platform.
5. Preserve single-process semantics. Wrap the model at one clear boundary, ensure every expected parameter participates correctly, and avoid `DataParallel` for new multi-GPU training code.
6. Shard input data correctly. For map-style data, use an appropriate distributed sampler and call `set_epoch`. For iterable data, shard by rank and worker.
7. Keep collective order identical across ranks. Rank-zero-only logging and I/O must not cause other ranks to skip required collectives.
8. Aggregate metrics by reducing numerators and denominators, then divide. Do not average per-rank averages when rank workloads can differ.
9. Make checkpoint semantics explicit. For DDP, coordinate rank-safe writes and restores. For FSDP or sharded models, use supported distributed state-dict and distributed-checkpoint workflows for the installed version.
10. Run `torchrun --standalone --nproc-per-node=2 scripts/ddp_smoke.py` to verify the local process environment, then run a tiny project-specific two-rank forward and backward test.
11. Add rank-tagged diagnostics around suspected hangs and use timeouts appropriate for debugging. Remove noisy diagnostics after fixing the synchronization defect.
12. Report the strategy, launcher command, backend, rank-to-device mapping, data sharding, metric reduction, checkpoint format, and tests run at each world size.

# Distributed rules

- Use one process per accelerator for DDP unless the platform documentation requires another mapping.
- Do not let only rank zero enter or skip a collective.
- Do not write the same checkpoint or log file concurrently from every rank without a coordination design.
- During gradient accumulation, use `no_sync()` only for non-boundary microbatches and verify the final synchronization.
- Do not compile a distributed wrapper blindly. For DDP, check the installed-version guidance and verify whether wrapping before compilation is needed for DDPOptimizer or whether compiling the inner module is the stable supported path. For FSDP, use its documented supported placement rather than assuming DDP behavior.
- Treat world-size changes on resume as a checkpoint-design requirement, not an incidental behavior.
- Fail loudly on mismatched tensor shapes, dtypes, or collective counts rather than allowing a hang to be the first signal.

# Definition of done

- A two-process local smoke test initializes, communicates, trains one step, and exits cleanly.
- Each sample is assigned according to the documented sharding policy.
- Distributed metrics match a single-process reference on a small deterministic case.
- Checkpoint save and load work at the tested world size, with world-size portability stated explicitly.
- No rank depends on uncoordinated file or network side effects.

Read [the distributed checklist](references/distributed-checklist.md) before adding FSDP, DTensor, or complex checkpointing.
