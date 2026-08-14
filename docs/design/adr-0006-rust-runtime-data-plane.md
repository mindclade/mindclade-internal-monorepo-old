# Rust owns the online and node data plane

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Mindclade internal monorepo

## Context

Per-request Go routing would make the durable control plane a latency and availability dependency. Python is the correct numerical runtime but not the preferred authority for network framing, process supervision, local resource budgets, or artifact transfer.

## Decision

Rust owns runtime gateway, runtime host, node agent, artifact proxy, signed-ticket validation, local admission, framing, streaming, cancellation, deadlines, load shedding, process supervision, local caches, byte transfer, and response multiplexing. Python owns final tensor-aware batching and model execution.

## Consequences

- A temporary control-plane outage has bounded cached-authority behavior.
- All Rust queues, allocations, tasks, and shutdown paths are bounded and supervised.
- Large payloads use descriptors/shared files/object references rather than Protobuf bodies.

## Enforcement

- Runtime qualification measures latency, cancellation, restart, memory, copies, queues, and cache behavior.
- No admitted request requires a live Go callback.

## Supersession

A later ADR must explicitly supersede this decision; implementation drift does not change the accepted architecture.
