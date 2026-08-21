# Training optimization

**Status:** bounded SGD and AdamW construction implemented for the CPU reference
trainer; custom algorithms, schedulers, distributed optimizers, and fused paths
remain scaffolded.

`build_optimizer` consumes a finite bounded collection of unique, leaf, trainable
CPU float32 `nn.Parameter` values. It rejects empty collections, duplicates,
frozen/non-parameter tensors, invalid ranges, and excessive parameter counts
before constructing `torch.optim.SGD` or `torch.optim.AdamW`. Foreach/fused
execution is disabled so this path remains a simple eager correctness oracle.
