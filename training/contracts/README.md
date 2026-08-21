# Training contracts

**Status:** the local CPU reference contracts are implemented; all unlisted
training contract modules remain target-state scaffolds.

The implemented surface contains exactly one `TrainingState`, one `StepResult`,
the `Task`/`TaskResult` objective boundary, and `SupervisedBatch`.

- A supervised batch is a non-empty, finite CPU float32 input/target pair with a
  shared positive leading dimension. Tensor storage remains caller-owned and no
  implicit copy, cast, or device transfer occurs.
- A task returns a differentiable scalar loss **sum** and its exact positive
  denominator. The trainer, not the task, normalizes an accumulation group.
- Training state records committed microbatches, optimizer steps, and samples.
  Failed or canceled groups and evaluation do not advance it.
- A step result contains only detached finite Python metrics in an immutable
  mapping. It distinguishes optimizer results from evaluation results.

These are process-local Python contracts. Cross-process training and checkpoint
messages remain owned by canonical schemas under `protocols/`. This package does
not own model, dataset, distributed, checkpoint, service, or deployment semantics.
