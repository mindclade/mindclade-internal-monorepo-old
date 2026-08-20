# Node agent

**Language:** Rust  
**Status in this archive:** implemented core; production qualification pending.

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

## Implemented core

The service contains ticketed stage execution, bounded artifact/checkpoint and
dataset transfer adapters, reference-cache authorization, subprocess/tool
supervision, diagnostics, hierarchical budgets, cancellation, and fail-closed
drain behavior. Tool invocations require absolute executables and enforce hard
timeout/output limits. Each invocation starts a Unix process group; timeout,
registration rejection, normal completion with surviving descendants, and
shutdown send TERM to the group, wait a bounded grace interval, then send KILL
and reap the direct child. Retained stdout/stderr acquire aggregate byte permits
before process creation and keep those permits until `ToolOutput` is dropped.

## Limitations

Promotion still requires connected provider tests, Linux/process-tree failure
injection, fuzz/concurrency and sanitizer qualification, measured load/resource
budgets, security review, and the release evidence referenced from
`PRODUCTION_READINESS.md`.
