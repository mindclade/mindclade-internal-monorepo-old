# TileLang acceleration is qualification gated

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Mindclade internal monorepo

## Context

Custom kernels can improve frontier-model efficiency but can also compile successfully while producing incorrect values, gradients, aliasing behavior, or shape-specific failures.

## Decision

PyTorch implementations are semantic references. TileLang kernels are selected only for a qualified signature covering operation, dtype, shape family, layout, device architecture, compiler/runtime version, numerical tolerances, gradients, and performance. Unknown or revoked signatures fall back.

## Consequences

- Autotuning is offline and produces immutable schedule evidence.
- Runtime requests never self-promote a schedule.
- Last-known-good and revocation records are required.

## Enforcement

- Presubmit/nightly/release lanes run parity, malformed-shape, NaN/Inf, noncontiguous, compile, and performance tests.
- Dispatch is fail-closed to reference/provider fallback.

## Supersession

A later ADR must explicitly supersede this decision; implementation drift does not change the accepted architecture.
