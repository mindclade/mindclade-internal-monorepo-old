# ADR-0016: Rust/Python inference batching boundary

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Online and batch model execution

## Context

Host/network admission requires bounded queues, deadlines, process state, and
resource reservations. Final model batching requires tensor shapes, padding,
KV/feature caches, compile buckets, CUDA graphs, diffusion/sample state, and
model-specific memory behavior. Duplicating the latter in Rust would create a
second numerical authority.

## Decision

Rust owns local admission queues, coarse manifest-declared compatibility
classes, request-envelope accounting, node resource reservations, backpressure,
load shedding, cancellation, process supervision, and response multiplexing.
Python/PyTorch owns final batch formation, packing/padding, shape buckets,
caches, compile/CUDA-graph selection, diffusion scheduling, and execution.

The Rust host may propose request envelopes and reject a Python `BatchPlan` that
violates a reserved resource envelope. Python remains authoritative about which
requests can share tensors.

## Consequences

Model compatibility rules have one owner, while the host can enforce safe local
limits without interpreting model internals. The IPC contract carries bounded
control messages and bulk-data descriptors rather than large tensors in
Protobuf.
