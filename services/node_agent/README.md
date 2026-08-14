# Node agent

**Language:** Rust  
**Status in this archive:** target-state composition scaffold; not a production implementation.

## Role

Shared node execution substrate for ingestion, preprocessing, training, checkpoint, and artifact/data transfer workloads.

## Owns

- reference database cache activation;
- subprocess/tool supervision;
- artifact and checkpoint transfer;
- dataset streaming/prefetch;
- CPU/RAM/disk/file/network budgets;
- node diagnostics and health;

## Does not own

- scientific search/curation semantics, training step state, global scheduling, or model policy;

## Dependencies

- `libs/rust/{runtime_core,bytes_io,object_store,artifact_cas,checkpoint_io,data_stream,worker_runtime,servicekit,telemetry}`;
- Go execution tickets and immutable reference manifests;

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

- hierarchical node/service/worker/request budgets and bounded subprocess output, caches, transfers, and blocking pools;

## Limitations

The Rust files in this scaffold reserve the intended package and build
boundaries. Promotion requires actual runtime implementation, connected tests,
fuzz/concurrency qualification, load and failure testing, security review, and
evidence referenced from `PRODUCTION_READINESS.md`.
