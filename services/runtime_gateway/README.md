# Runtime gateway

**Language:** Rust  
**Implementation status:** executable transport implemented; production qualification pending.

## Role

Latency-sensitive online inference network boundary that operates from bounded
Go-issued authority without a synchronous Go call per request.

## Implemented core

The current source implements the model-independent runtime core for:

- signed admission/execution authority validation and expiry checks;
- monotonic route and revocation snapshot installation;
- deterministic local route selection from immutable snapshots;
- local admission and bounded grant-budget accounting;
- coarse compatibility grouping without taking ownership of tensor semantics;
- bounded response multiplexing and stream state;
- cancellation, deadlines, readiness, drain, and fail-closed state transitions;
- structured runtime telemetry seams.

The HTTP `POST /v1/runtime/resolve` endpoint is a bounded resolver API. It
validates admission authority, selects a route, releases its local resolver
permit, and returns the selected host endpoint with `200 OK`. The legacy
`POST /v1/runtime/dispatch` alias retains its `202 Accepted` response and emits
`Deprecation: true` plus a successor `Link` header.

Execution uses the `RuntimeExecution.Execute` server-streaming gRPC method.
That path keeps its admission permit through the terminal host status, bridges
client disconnects and ticket deadlines to a host cancellation command, and
holds weighted request/response byte reservations while bytes are retained.
Execution is disabled unless `MINDCLADE_RUNTIME_EXECUTION_ENABLED=true`.

The integration, transport, and shutdown tests exercise the core contracts.
Provider and hardware qualification remain release requirements.

## Owns

- runtime authentication/session framing;
- execution/admission grant verification;
- local route snapshot lookup;
- coarse compatibility classes and local resource admission;
- SSE/streaming, deadlines, cancellation, response multiplexing, and load shedding.

## Does not own

- global quota or entitlement policy;
- model numerical execution;
- final tensor-aware batching;
- model release state;
- durable job orchestration;
- scientific preprocessing.

Those authorities remain in Go or Python according to the canonical language
boundary documented in `docs/architecture/system-design-reference.md`.

## Dependencies

- `libs/rust/{runtime_core,worker_protocol,ipc,servicekit,telemetry,gpu_host}`;
- `protocols/.../runtime/v1` tickets, grants, routes, revocation, and worker contracts;
- `runtime_host` through a bounded local control protocol.

## Failure semantics

- already-admitted work can continue while its local authority remains valid;
- new work without valid unexpired authority is rejected;
- stale/revoked policy is rejected locally;
- expired route state enters fail-closed/drain behavior;
- queues and budgets are bounded;
- drain removes readiness before admitted work is terminated.

## Production qualification still required

Promotion requires the pinned Rust/Bazel/Nix toolchain, Tonic/Tokio transport
qualification, cross-language fixtures, fuzz/concurrency tests, load and failure
tests, security review, SLO evidence, and deployment rollback evidence. See
`PRODUCTION_READINESS.md`.
