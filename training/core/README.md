# Training core

**Status:** deterministic eager float32 reference trainer implemented for explicit
CPU/CUDA placement, with a bounded reduction interface for the implemented DDP
subset. Mixed precision, compilation, and service execution remain separate.
Bounded resume is implemented separately under
[`training/checkpointing`](../checkpointing/README.md).

`Trainer` is the sole optimizer-lifecycle authority. It accepts a finite bounded
sequence of `SupervisedBatch` values and groups them by a bounded accumulation
window. Each group performs:

1. `zero_grad(set_to_none=True)`;
2. all bounded forwards and loss-sum collection;
3. integer reduction of global counts, then one backward over the summed group objective using the
   reducer-owned scale and exact global denominator;
4. required finite same-device float32 gradient validation;
5. optional bounded norm clipping;
6. optimizer step, then optional scheduler step;
7. gradient clearing and atomic progress-counter advancement.

The final short accumulation group uses its own exact denominator. Cancellation
is checked before and after forward/backward work and before optimizer mutation.
Failed or canceled pre-step groups clear gradients and do not advance state.
Evaluation uses `eval()` with `inference_mode()`, restores the caller's model mode,
does not touch optimizer/scheduler/state, and returns detached totals.

The caller places model and batches explicitly on one CPU or CUDA device; no casts
or transfers are hidden in the trainer. Local reduction is the default. DDP uses
the separately owned reducer and wrapper and remains single-owner per process.
The trainer itself makes no AMP, checkpoint orchestration, throughput, or
production deployment claim.

An optimizer call is an irreversible transaction boundary. If the optimizer,
finite post-step validation, scheduler, or progress commit raises after that
boundary begins, the `Trainer` becomes poisoned and rejects further training or
evaluation. Callers must restore a verified checkpoint into fresh model,
optimizer, and trainer objects; retrying the same live objects could apply an
update twice or continue from partially mutated scheduler state.
