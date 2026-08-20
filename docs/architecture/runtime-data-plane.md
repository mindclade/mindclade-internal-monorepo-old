# Runtime data plane

The runtime data plane is Rust because it owns latency-sensitive networking,
local admission, byte movement, node resources, and process supervision.

## Components

```text
runtime_gateway
  authentication boundary, signed-grant validation, route snapshot cache,
  bounded resolver API, local admission, request framing, gRPC streaming,
  deadlines, cancellation,
  load shedding, response multiplexing

runtime_host
  model-slot lifecycle, Python process supervision, coarse request grouping,
  host/GPU reservation, control/data IPC, drain and restart

node_agent
  reference/artifact cache, external process supervision, checkpoint/data
  transfer, resource telemetry, diagnostics, local cleanup

artifact_proxy
  tenant-scoped CAS streaming, ranges, digest verification, signed URLs,
  local cache, atomic manifest publication
```

## Runtime contract

Go publishes immutable route snapshots and signed admission/execution tickets.
Rust validates signature, key validity, policy/revocation epoch, route version,
model/runtime bundle digests, tenant/artifact scope, deadline, resource budget,
and fencing token without a synchronous policy lookup.

Route resolution and execution are separate contracts. HTTP
`POST /v1/runtime/resolve` consumes only the bounded resolver admission and
returns a selected endpoint. gRPC `RuntimeExecution.Execute` is the execution
lifecycle: the gateway retains admission until a terminal worker status and
propagates cancellation, client disconnect, and the signed ticket deadline
through the host to the process-isolated worker.

## Node-wide budget

The hierarchy is node -> service -> worker -> request -> operation, plus shared
caches/background tasks. Reservations cover resident and pinned memory, shared
memory, buffer pools, local disk, file descriptors, object-store requests,
queued requests, processes, CPU threads, GPU estimate, checkpoint staging, and
telemetry spool.

Network bodies and retained output streams additionally acquire weighted byte
permits derived from the pod memory budget. Request-count limits protect task
and connection overhead; byte permits protect aggregate resident buffering.

## IPC

Control messages use bounded Protobuf over gRPC or Unix-domain sockets. Large
batches/tensors/artifacts use shared-memory, file-descriptor, local-file, or
object-reference descriptors containing owner, offset, length, type/shape,
digest, mode, generation, and lifetime lease.

## Worker lifecycle

```text
Created -> Starting -> Ready -> Leased -> Running -> Draining
        -> Committing -> Completed
failure: Recovering -> Ready or Failed
administrative: Cancelling -> Cancelled
```

All tasks belong to a supervised task tree. Blocking work uses bounded pools;
queues have capacity; waits honor cancellation/deadlines; shutdown has a fixed
budget.

## Control-plane outage

Fresh cached routes and valid bounded grants continue. Already-admitted work
continues. New work without valid authority is rejected. Expired route or
revocation state drains and fails closed. A bounded local usage/telemetry spool
may bridge a transient outage; exhaustion stops admission.
