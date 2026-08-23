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
- enforces `WorkerLimits.maximum_concurrent_executions` with a non-blocking
  semaphore, so a caller that arrives while the bound is saturated is shed with
  `ResourceExhausted` rather than entering the engine — the per-call
  `maximum_pending_requests` check bounds one call, not concurrent callers. The
  default is one; a deployment that means to run more passes `limits` built from
  its own `WorkerProcessConfig.maximum_concurrent_executions`;
- delegates **final tensor-aware batching** to a Python `BatchPlanner`;
- enforces request-count and estimated GPU-memory limits on returned plans;
- rejects a plan that repeats a request, schedules one this call never admitted,
  or substitutes a different payload under an admitted id, **before** that batch
  reaches the engine — `BatchPlan.validate` compares ids and digests and never
  re-runs `InferenceRequest.validate`, so unvalidated work would otherwise reach
  the model first;
- invokes an injected `ModelEngine` rather than embedding model-family logic;
- requires each batch's results to answer exactly that batch, then validates
  exactly one terminal result per admitted request, by count as well as by
  identity;
- translates the bare `ValueError` that `serving.contracts` and `config.py`
  raise, along with every rejection it makes itself, into the shared
  `libs.python.errors` contract, so nothing untyped crosses the Rust
  supervision boundary;
- rejects new work while draining.

`config.py` provides bounded configuration. `lifecycle.py` re-exports the shared
`libs.python.worker_runtime` lifecycle, as every other worker in
`services/workers` does: it counts in-flight executions, so `stop()` fails
closed while an engine call is still outstanding rather than letting the
supervisor believe the worker is quiescent. `ipc.py` is the production process
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
