<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Architecture](../docs/README.md) · [Maturity](../SCAFFOLD_STATUS.md)

# Model training

> **Maturity:** Scaffolded; no production training capability is claimed here.
> **Primary implementation:** Python and PyTorch.

`training/` reserves the authoritative contracts and reusable mechanisms for
training state, engines, distributed plans, checkpoints, optimization, runtime
coordination, and task objectives.

## What's here

| Path | Responsibility |
| --- | --- |
| [`contracts/`](contracts/) | Stable training inputs, outputs, state, and compatibility |
| [`core/`](core/) | Training state machine and reusable orchestration semantics |
| [`engines/`](engines/) | Framework and execution-engine adapters |
| [`distributed/`](distributed/) | Parallelism and distributed execution plans |
| [`checkpointing/`](checkpointing/) | Save, restore, compatibility, and resume orchestration |
| [`optim/`](optim/) | Optimizers, schedules, and optimization policy |
| [`runtime/`](runtime/) | Reusable runtime mechanisms for training workers |
| [`tasks/`](tasks/) | Task-specific objectives and data/model integration |
| [`cli/`](cli/) | Training inspection and invocation surfaces |

## Boundary

- Training owns model-update semantics; model definitions remain in
  [`models/`](../models/), and data semantics remain in [`data/`](../data/).
- Durable fleet scheduling and run authority belong in
  [`control/`](../control/).
- Deployable worker composition belongs under [`services/workers/`](../services/workers/).
- Checkpoint identity, compatibility, resume behavior, and failure semantics
  must be explicit and independently testable.

## Start here

- [Training architecture](../docs/architecture/training.md)
- [Distributed training](../docs/architecture/distributed-training.md)
- [Checkpointing](../docs/architecture/checkpointing.md)

## Promotion bar

Promotion requires named ownership, stable contracts, deterministic or
explicitly statistical behavior, bounded resources and cancellation, numerical
and resume evidence, Bazel ownership, compatibility and rollback policy, and
current qualification recorded in [`components.toml`](../components.toml).
