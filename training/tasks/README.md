# Training tasks

**Status:** supervised mean-squared-error reference task implemented; every other
task family remains scaffolded.

`SupervisedMSETask` calls an injected `nn.Module` with the batch inputs, requires
a finite CPU float32 tensor whose shape exactly matches the targets, and returns
sum-reduced squared error with `targets.numel()` as the denominator. It never
changes model mode, moves tensors, normalizes across a batch, or owns optimizer
behavior. Those choices remain visible to the authoritative trainer.
