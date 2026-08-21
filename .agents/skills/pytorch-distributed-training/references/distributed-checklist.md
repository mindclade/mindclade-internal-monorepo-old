# Distributed training checklist

## Strategy selection

- **DDP:** replicate the model, shard data, synchronize gradients. Start here for ordinary multi-GPU or multi-node data parallelism.
- **FSDP:** shard parameters, gradients, and optimizer state when replication does not fit or communication-memory tradeoffs justify it.
- **DTensor or tensor parallelism:** partition tensor dimensions and model computation when the model itself must span devices.
- **Pipeline parallelism:** use only when stage partitioning and microbatch scheduling fit the model and operational complexity is acceptable.

## Initialization

Verify:

- launcher-provided `RANK`, `LOCAL_RANK`, and `WORLD_SIZE`;
- backend availability;
- local device selection before accelerator allocations;
- process-group timeout and teardown;
- deterministic rank-tagged logging during diagnosis.

## Data

For map-style datasets, use a distributed sampler and call `sampler.set_epoch(epoch)` before each shuffled epoch. Define `drop_last` and padding semantics. For iterable datasets, shard by both rank and DataLoader worker.

## Metrics

Reduce totals, not already-normalized values:

```text
global_loss = all_reduce(local_loss_sum) / all_reduce(local_item_count)
global_accuracy = all_reduce(local_correct) / all_reduce(local_total)
```

Use a dtype and device supported by the backend.

## Hangs

Common causes include:

- collectives called in different orders;
- one rank exits or throws before peers;
- rank-zero-only branches enclosing a collective;
- incompatible tensor metadata;
- DataLoader workers deadlocking before the training collective;
- filesystem or rendezvous delays mistaken for a collective hang.

Add logs immediately before and after collective boundaries with rank and step numbers. Reproduce with two local processes and the smallest batch.

## Checkpointing

For DDP, model weights are replicated but optimizer and progress state still need coordinated save and load. For FSDP, use the state-dict and `torch.distributed.checkpoint` APIs recommended for the installed version. Test whether the format supports a different world size before promising elastic resume.

## Compilation

PyTorch guidance recommends compiling the inner module rather than DDP or FSDP wrappers when using `torch.compile`. Confirm the installed release's limitations and compare eager versus compiled distributed results.

Official references:

- Distributed overview: https://docs.pytorch.org/tutorials/beginner/dist_overview.html
- DDP: https://docs.pytorch.org/docs/stable/generated/torch.nn.parallel.DistributedDataParallel.html
- FSDP: https://docs.pytorch.org/docs/stable/fsdp.html
- DTensor: https://docs.pytorch.org/docs/stable/distributed.tensor.html
- Distributed checkpoint: https://docs.pytorch.org/docs/stable/distributed.checkpoint.html
