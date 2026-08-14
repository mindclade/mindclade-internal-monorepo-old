# Training architecture

Mindclade owns the trainer contracts, training state, task/objective semantics,
topology policy, checkpoint schema, data semantics, and numerical qualification.
Execution engines are adapters.

## Package model

```text
training/contracts/   stable protocols and typed records
training/core/        one authoritative trainer, state machine, step executor
training/engines/     native PyTorch, TorchTitan, Fabric adapters
training/distributed/ topology, groups, plans, collectives, diagnostics
training/checkpointing/ DCP orchestration, manifests, resume, reshard
training/optim/       optimizer/scheduler/gradient semantics
training/runtime/     precision, memory, compilation, hooks, callbacks,
                      telemetry, resilience, rollouts
training/tasks/       causal LM, MoE, diffusion, biology, multimodal, RL
```

There is exactly one authoritative `Trainer`, `TrainingState`, `StepResult`, and
lifecycle coordinator.

## Task contract

A task owns batch interpretation, forward inputs, objective/loss terms,
reductions, metrics, auxiliary state, evaluation requests, and task-specific
checkpointables. This supports dense/MoE language models, diffusion,
multimodal/biological models, and reinforcement learning without branching the
trainer.

## Engine adapters

- `native`: production PyTorch-native reference and execution path;
- `torchtitan`: large-scale adapter consuming Mindclade contracts;
- `fabric`: developer runtime and telemetry integration, not layered over
  TorchTitan.

Optional providers such as Transformer Engine or TorchAO sit behind narrow
adapters and qualification.

## Hooks and callbacks

Hooks are synchronous, ordered, rank-aware, potentially mutating, and part of
the numerical critical path. Callbacks are non-mutating event consumers,
normally asynchronous, backpressured, retryable, and forbidden from invoking
collectives. Types and runtime checks enforce the distinction.

## Launch and control

Go admits quota/capacity and creates the durable run/job. Kubernetes Kueue and
JobSet represent resource admission and coordinated workload topology. Rust
node agents stage datasets, weights, references, and checkpoints and enforce
node budgets. Python owns process groups, model state, and numerical progress.

## Completion

A training run is not promotable until checkpoint integrity, deterministic
resume, numerical evidence, evaluation suites, safety/security gates, build and
toolchain provenance, cost/performance evidence, and rollback artifacts are
attached.
