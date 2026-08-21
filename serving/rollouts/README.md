# Serving / Rollouts

**Status:** implemented provider-neutral runtime; actor/model qualification pending.

This package owns immutable policy snapshots, monotonic revision synchronization, bounded policy
residency, deterministic per-trajectory seed derivation, compatible batching, exact actor result
cardinality, and bounded trajectory validation. It fails readiness when the active snapshot expires
and rejects work for any policy other than the fresh active digest.

The injected actor owns model-specific inference and environment interaction. Go owns durable
learner/fleet policy and retries; Rust owns signed ticket verification, fencing, resource budgets,
process supervision, and bulk transport. This package does not implement those responsibilities or
claim statistical/model quality without connected evaluation evidence.
