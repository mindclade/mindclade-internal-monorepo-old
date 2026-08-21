# Artifact proxy

**Language:** Rust  
**Status in this archive:** core implementation complete; provider/network composition and
deployment qualification pending.

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

The reusable core implements validated grants, bounded ranges, provider-backed publication/read,
cache behavior, and component tests. The checked-in binary is deliberately a fail-closed
composition seam: no listener starts until deployment wiring supplies a qualified object-store
provider, network transport, identity, and telemetry exporter. Connected concurrency, load,
failure, security, image, and rollback evidence remains required by `PRODUCTION_READINESS.md`.
