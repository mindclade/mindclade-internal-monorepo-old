# Runtime gateway

**Language:** Rust  
**Implementation status:** implemented core; production qualification pending.

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

The integration and shutdown tests exercise the core contracts. The binary and
provider/network leaves are intentionally not described as production-qualified.

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
