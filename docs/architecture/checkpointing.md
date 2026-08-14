# Checkpointing

Checkpointing has one semantic authority and several deliberately narrow
implementations.

## Ownership

| Component | Owns |
|---|---|
| `models/*` | Model-specific state names, transformations, compatibility |
| `training/checkpointing` | State registry, save/load plans, DCP orchestration, atomic commit, resume/reshard policy |
| `libs/rust/checkpoint_io` | High-throughput bytes, staging, verification, multipart transfer, repair |
| `services/artifact_proxy` | Durable storage, tenant access, retention, encryption, signing |
| `protocols/` | Canonical manifest and artifact-reference wire schemas |

## Registered state

The training state registry supports model, optimizer, scheduler, scaler,
EMA, RNG, data-loader position, task state, curriculum, sampler, rollout/replay
state, and arbitrary checkpointable components.

## Commit protocol

```text
freeze/checkpoint safe point
  -> produce distributed save plan
  -> write content-addressed shards to staging
  -> verify per-object digest and metadata
  -> write complete manifest
  -> atomically publish commit record
  -> asynchronously update catalog/retention
```

Only a committed manifest is restorable. A timed-out attempt leaves unreferenced
staging objects eligible for cleanup.

## Manifest

The manifest records schema version, run/step/attempt, code/config/toolchain
and environment digests, topology fingerprint, parameter and optimizer layouts,
all registered components, RNG and data position, object digests/sizes,
compression/encryption, parent checkpoint, commit time, and compatibility
policy.

## Async saves

Asynchronous saves consume host-memory and transfer budgets explicitly. Rust
acquires node reservations before staging; Python/DCP owns tensor/state
semantics. Backpressure may coalesce or defer requests but cannot silently drop
a required preemption or release checkpoint.

## Restore

Restore verifies manifest/signature/digests, resolves compatibility, computes
load/reshard plans, restores arbitrary components, validates data/RNG position,
and emits an environment/resume report. Partial model-only loads are explicit
operations, not accidental behavior.
