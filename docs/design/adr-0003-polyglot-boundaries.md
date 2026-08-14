# Assign languages by authority, not preference

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Mindclade internal monorepo

## Context

Frontier biological-model systems combine durable fleet policy, latency-sensitive networking and byte I/O, scientific/numerical code, accelerator kernels, and browser products. A single-language mandate would put critical logic in the wrong runtime.

## Decision

Go owns fleet control and durable policy. Rust owns online/runtime data plane and node execution. Python/PyTorch owns scientific and numerical semantics. TileLang owns qualification-gated accelerator kernels. TypeScript owns browser applications and generated web clients.

## Consequences

- Cross-language contracts require canonical schemas and golden vectors.
- No language independently reimplements another plane’s policy.
- Process boundaries are preferred over embedding Python in long-lived Rust hosts.

## Enforcement

- Dependency analysis rejects boundary violations.
- ADRs are required to move an authoritative responsibility.

## Supersession

A later ADR must explicitly supersede this decision; implementation drift does not change the accepted architecture.
