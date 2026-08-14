# Model worker

**Language:** Python/PyTorch  
**Status:** implemented adapter core; model-engine and hardware qualification remain separate.

## Role

The model worker is the Python-owned numerical boundary behind the Rust runtime
host. Rust admits work, enforces node/process budgets, supervises the Python
process, and transports bounded control/bulk descriptors. Python remains the
final authority for tensor compatibility, batch construction, model loading,
sampling/diffusion/confidence, and GPU execution.

## Implemented adapter core

`executor.py` provides a bounded `ModelWorker` that:

- validates every admitted `InferenceRequest` before planning;
- delegates **final tensor-aware batching** to a Python `BatchPlanner`;
- enforces request-count and estimated GPU-memory limits on returned plans;
- rejects duplicate or omitted request scheduling;
- invokes an injected `ModelEngine` rather than embedding model-family logic;
- validates exactly one terminal result per admitted request;
- rejects new work while draining.

`config.py` and `lifecycle.py` provide bounded configuration and explicit
startup/readiness/drain/stop semantics. Package-local tests use deterministic
fake planner/engine implementations to prove the adapter contract without
pretending a scientific model is implemented.

## Does not own

- online network admission, route policy, ticket issuance, or global quotas;
- Rust node/process/GPU resource authority;
- scientific preprocessing such as MSA/template/ligand preparation;
- model-family architecture or checkpoint semantics.

Those remain in Go control policy, Rust runtime/node services, `preprocessing/`,
`models/`, and `training/` respectively.

## Production promotion

Production use additionally requires a qualified model engine, generated/runtime
protocol adapter, real GPU-memory calibration, cancellation/deadline propagation,
Rust-host IPC integration, numerical qualification, and Bazel/Nix image/release
evidence. See `PRODUCTION_READINESS.md`.
