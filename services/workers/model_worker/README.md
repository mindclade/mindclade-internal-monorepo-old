# Model worker

**Language:** Python/PyTorch  
**Status:** executable reference adapter implemented; scientific model and GPU qualification remain separate.

## Role

The model worker is the Python-owned numerical boundary behind the Rust runtime
host. Rust admits work, enforces node/process budgets, supervises the Python
process, and transports bounded control/bulk descriptors. Python remains the
final authority for tensor compatibility, batch construction, model loading,
sampling/diffusion/confidence, and GPU execution.

## Implemented adapter

`executor.py` provides a bounded `ModelWorker` that:

- validates every admitted `InferenceRequest` before planning;
- delegates **final tensor-aware batching** to a Python `BatchPlanner`;
- enforces request-count and estimated GPU-memory limits on returned plans;
- rejects duplicate or omitted request scheduling;
- invokes an injected `ModelEngine` rather than embedding model-family logic;
- validates exactly one terminal result per admitted request;
- rejects new work while draining.

`config.py` and `lifecycle.py` provide bounded configuration and explicit
startup/readiness/drain/stop semantics. `ipc.py` is the production process
boundary: it accepts only 1 MiB length-prefixed canonical runtime protobufs on
a permission-restricted Unix socket, bounds pending and executing requests,
checks monotonic command sequences, propagates deadlines/cancellation, and
does not acknowledge cancellation until the execution thread has stopped.

`serving/model_worker/reference.py` supplies the real PyTorch execution path
used for integration and qualification. It verifies the canonical model-bundle
manifest and every member digest, loads only safetensors, validates and hashes
the admitted input range before creating a tensor, executes the deterministic
`reference.affine.v1` operation, and atomically publishes a read-only output.
This proves Rust-to-Python execution without representing the affine fixture as
a scientific production model.

The process is launched with exactly two environment variables:

- `MINDCLADE_MODEL_WORKER_CONFIG`: absolute path to the versioned JSON config;
- `MINDCLADE_MODEL_WORKER_SOCKET`: absolute path to the worker Unix socket.

Schema version 1 requires immutable bundle identity/root, output and allowed
input roots, `cpu` or `cuda`, pending/concurrent/input/output limits, I/O and
cancellation bounds, and reference chunk/iteration bounds. Unknown, missing,
relative, symlinked, or out-of-range configuration fails startup.

## Does not own

- online network admission, route policy, ticket issuance, or global quotas;
- Rust node/process/GPU resource authority;
- scientific preprocessing such as MSA/template/ligand preparation;
- model-family architecture or checkpoint semantics.

Those remain in Go control policy, Rust runtime/node services, `preprocessing/`,
`models/`, and `training/` respectively.

## Production promotion

The generated Python protobufs have Rust golden-wire parity, and the Bazel
`//services/runtime_host:execution_transport` test supervises the executable
worker through admission, UDS execution, and verified output. Production use
still requires a qualified scientific model engine, real GPU-memory
calibration, numerical qualification, and Bazel/Nix image/release evidence.
See `PRODUCTION_READINESS.md`.
