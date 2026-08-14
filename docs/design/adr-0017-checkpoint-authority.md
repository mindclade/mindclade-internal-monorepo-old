# ADR-0017: Single checkpoint authority with split mechanics

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Training/model checkpoint lifecycle

## Context

Checkpoint concerns appear in model compatibility, trainer state, distributed
save/load/reshard planning, high-throughput transfer, durable storage, registry
metadata, and wire schemas. Competing end-to-end formats would break resume,
world-size changes, reproducibility, and release evidence.

## Decision

- Python `training/checkpointing` owns the authoritative state registry,
  DCP-based save/load/reshard semantics, atomic commit, retention intent, and
  resume policy.
- Model packages own model-specific state naming and compatibility transforms.
- Rust owns byte staging, transfer, verification, repair mechanics, and explicit
  host-memory/network/disk budgets.
- The artifact proxy owns durable protected bytes and access enforcement.
- `protocols/` owns canonical checkpoint/artifact manifest schemas.

Only a fully committed manifest is restorable. Every checkpoint records schema,
run/step, topology, parameter/optimizer layouts, arbitrary registered
components, RNG, data position, config/code/toolchain provenance, object
digests, and compatibility policy.

## Consequences

Async save is not considered free: CPU staging and transfer consume the
node-wide budget. Cross-topology restore and arbitrary checkpointable components
remain possible without model-family-specific storage systems.
