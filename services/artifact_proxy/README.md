# Artifact proxy

**Language:** Rust  
**Status in this archive:** target-state composition scaffold; not a production implementation.

## Role

Tenant-scoped high-throughput byte plane for content-addressed artifacts, checkpoints, model weights, range reads, and signed download access.

## Owns

- local validation of signed artifact grants;
- namespace/prefix enforcement;
- range validation and bounded streaming;
- digest and manifest verification;
- local cache integration;
- response streaming, cancellation, drain, and telemetry;

## Does not own

- artifact catalog, retention policy, tenant entitlement, release policy, or scientific serialization semantics;

## Dependencies

- `libs/rust/{artifact_cas,object_store,content_digest,manifests,runtime_core,servicekit,telemetry}`;
- `protocols/` artifact and grant contracts;
- Go registry/control-plane policy snapshots;

## Lifecycle

The service must start dependencies in order, expose liveness separately from
readiness, reject new work before drain, propagate cancellation/deadlines,
finish or safely abandon admitted work, flush bounded telemetry, and stop all
owned tasks/processes within a declared shutdown budget.

## Determinism and correctness

Every accepted artifact/ticket/route/status uses canonical versioned contracts.
Resource allocation is reserved before use, queues are bounded, stale fencing
tokens cannot commit, and unverified bytes are never accepted.

## Resource and operational limits

- maximum range and response bytes;
- bounded connections, buffers, object-store operations, cache bytes, file descriptors, and spool bytes;

## Limitations

The Rust files in this scaffold reserve the intended package and build
boundaries. Promotion requires actual runtime implementation, connected tests,
fuzz/concurrency qualification, load and failure testing, security review, and
evidence referenced from `PRODUCTION_READINESS.md`.
