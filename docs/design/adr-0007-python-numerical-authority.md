# Python/PyTorch owns scientific and numerical semantics

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Mindclade internal monorepo

## Context

Model architecture, objectives, distributed state, tensor batching, biological preprocessing, evaluation, and rapidly evolving research logic require direct alignment with PyTorch and the scientific Python ecosystem.

## Decision

Python owns model/training/inference/evaluation numerics and scientific transformations. Rust may accelerate bounded parsing, data movement, and small leaf primitives, but it does not become a second authority for model or biological semantics.

## Consequences

- One authoritative trainer, training state, task contract, and checkpoint schema are maintained in Python.
- TorchTitan and Fabric are adapters behind Mindclade-owned contracts.
- PyO3 remains a leaf adapter; long-lived workers use process IPC.

## Enforcement

- Numerical parity and determinism tests gate changes.
- Model-serving parity is evidence, not assumed from shared code names.

## Supersession

A later ADR must explicitly supersede this decision; implementation drift does not change the accepted architecture.
