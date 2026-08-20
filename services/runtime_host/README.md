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
- bounded length-delimited Protobuf IPC to the process-isolated model worker;
- diagnostics and telemetry seams.

The host deliberately does not embed Python and does not implement model or
tensor semantics. Its streaming execution service revalidates the ticket,
forwards start/cancel commands to the selected worker, validates monotonic
worker identities and fencing tokens, and fails closed by terminating an
unresponsive or protocol-invalid worker. Integration, IPC, process-tree, and
shutdown tests cover these contracts.

An optional preloaded worker is enabled only when the complete launch contract
is present: `MINDCLADE_RUNTIME_MODEL_BUNDLE_DIGEST`,
`MINDCLADE_RUNTIME_MODEL_WORKER_EXECUTABLE`,
`MINDCLADE_RUNTIME_MODEL_WORKER_SOCKET`, and
`MINDCLADE_RUNTIME_MODEL_WORKER_CONFIG`. The host validates the bounded,
absolute paths and starts the process with a cleared environment containing
only the worker-facing socket and config variables. Partial configuration is a
bootstrap error.

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
