# Training core

**Status:** deterministic single-process CPU reference trainer implemented;
distributed, mixed-precision, compilation, checkpointing, and service execution
remain separate scaffolds.

`Trainer` is the sole optimizer-lifecycle authority. It accepts a finite bounded
sequence of `SupervisedBatch` values and groups them by a bounded accumulation
window. Each group performs:

1. `zero_grad(set_to_none=True)`;
2. all bounded forwards and loss-sum collection;
3. backward of each loss sum divided by the group's total denominator;
4. required finite CPU float32 gradient validation;
5. optional bounded norm clipping;
6. optimizer step, then optional scheduler step;
7. gradient clearing and atomic progress-counter advancement.

The final short accumulation group uses its own exact denominator. Cancellation
is checked before and after forward/backward work and before optimizer mutation.
Failed or canceled pre-step groups clear gradients and do not advance state.
Evaluation uses `eval()` with `inference_mode()`, restores the caller's model mode,
does not touch optimizer/scheduler/state, and returns detached totals.

The implementation is intentionally CPU float32 and single-owner. It makes no
AMP, accelerator, distributed, checkpoint/resume, throughput, or production
deployment claim.
