# Runtime host

**Language:** Rust  
**Implementation status:** implemented core; production qualification pending.

## Role

Node-local authority around process-isolated Python/PyTorch model workers and
model slots.

## Implemented core

The current source implements model-independent host mechanisms for:

- independent execution-ticket/authority revalidation;
- hierarchical node/service/worker/request resource reservations;
- Python worker process lifecycle and bounded restart/supervision policy;
- GPU/model-slot reservation and compatibility envelopes;
- control-message versus bulk-buffer descriptor separation;
- cancellation, deadlines, readiness, drain, and shutdown;
- diagnostics and telemetry seams.

The host deliberately does not embed Python and does not implement model or
tensor semantics. Integration and shutdown tests cover the core contracts.

## Owns

- worker process lifecycle and restart policy;
- GPU, host-memory, pinned/shared-memory, queue, and process reservations;
- model-slot compatibility and coarse batch envelopes;
- IPC control and bulk-data descriptors;
- cancellation, deadline, drain, and diagnostics.

## Does not own

- final tensor batch construction;
- model architecture or sampling semantics;
- global scheduling or quota policy;
- scientific preprocessing;
- training numerical state.

## Dependencies

- `libs/rust/{worker_runtime,worker_protocol,ipc,gpu_host,runtime_core,servicekit,telemetry}`;
- canonical runtime protocols;
- Python serving/model-worker contracts through process-isolated IPC.

## Production qualification still required

Promotion requires compilation with the pinned Rust toolchain, process/IPC
failure injection, OS-specific bulk IPC qualification, GPU-memory and restart
stress tests, fuzz/Miri/sanitizer where applicable, security review, SLO/load
evidence, and deployment rollback evidence. See `PRODUCTION_READINESS.md`.
