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
cache behavior, and component tests.

The binary composes and stays resident (`src/bootstrap.rs`), serving `/healthz`, `/readyz`, and
`/metrics`. It is still fail-closed, but by exiting 78 rather than by declining to start: a missing
or invalid environment variable, or an object store that does not answer within five seconds, ends
the process before the listener binds.

What it does **not** serve is artifact bytes. The tenant-scoped byte plane has no wire contract in
`protocols/` — the `ArtifactService` there is the control plane's catalog, which this service does
not own — so reads and writes still arrive only through in-process calls. Readiness is scoped to
the operational plane; a ready instance is not evidence that any caller can fetch bytes from it.
Network transport, identity, and a real telemetry exporter remain deployment-supplied.

The proxy verifies digests, on CAS read and on cache insert. It does **not** verify manifests.

Connected concurrency, load, failure, security, image, and rollback evidence remains required by
`PRODUCTION_READINESS.md`.
