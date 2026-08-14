# Runtime authority and unified stage execution

## Online request path

```text
Go control plane
  ├─ publishes signed immutable route snapshot
  ├─ publishes signed revocation snapshot
  └─ issues bounded admission grant
             │
             ▼
Rust runtime gateway
  ├─ verifies grant locally
  ├─ verifies policy/route/revocation freshness
  ├─ resolves route from the cached signed snapshot
  ├─ enforces local request/concurrency/unit budgets
  ├─ applies load shedding/backpressure
  └─ multiplexes bounded streaming responses
             │
             ▼
Rust runtime host
  ├─ independently validates execution ticket + revocation state
  ├─ reserves hierarchical host/GPU resources
  ├─ supervises process-isolated Python workers
  ├─ enforces control-frame and bulk-descriptor bounds
  └─ rejects stale fencing tokens on commit
             │
             ▼
Python/PyTorch worker
  ├─ final tensor-aware batching
  ├─ model-specific memory/cache/layout logic
  ├─ model execution and sampling
  └─ qualified TileLang kernels
```

No accepted online request requires a synchronous callback to Go. A control
plane outage therefore has bounded semantics: admitted work continues, valid
unexpired grants may be consumed within their local budgets while required
snapshots remain fresh, work without valid authority is rejected, and stale
route/revocation state causes admission to fail closed or drain.

## MCCE1 canonical signed claims

Go and Rust do not sign language-private struct serialization. MCCE1 is a small
canonical claims encoding used for detached signatures:

1. document header `MCCE1/<document-type>\0`;
2. each field is `u16(key length) || key || u32(value length) || value`;
3. integers are fixed-width big-endian;
4. set-like string collections sort lexically before encoding;
5. nested documents are included as their canonical bytes;
6. signatures cover the exact canonical byte sequence;
7. a key ID and algorithm travel outside the claims bytes.

The canonical wire service representation remains Protobuf. MCCE1 exists only
to make signing bytes deterministic and independently implementable.

## Fencing

A ticket contains a non-zero monotonic fencing token. The worker runtime retains
that token for the active lease and requires the same token at commit. A stale
or duplicated worker cannot publish outputs after a replacement has acquired a
newer fence. Durable stores must enforce the same rule at their final commit
boundary; in-process validation is defense in depth, not the sole authority.

## Unified durable stage

Ingestion, curation, preprocessing, reference building, batch inference,
evaluation, training, checkpoint/artifact transfer, rollout and simulation use
one durable `StageSpec` / `StageAttempt` vocabulary. Go owns DAG state and
attempt policy. Rust owns ticket/resource/process execution. Python owns the
scientific/numerical engine when the stage requires it.

```text
StageSpec
  ├─ canonical stage ID + kind + operation
  ├─ immutable ArtifactRef inputs
  ├─ output namespace
  ├─ resolved configuration digest
  ├─ optional reference-database snapshot digest
  ├─ execution resource budget
  ├─ timeout / max attempts
  └─ dependency stage IDs

StageAttempt
  ├─ run/job
  ├─ attempt number
  ├─ fencing token
  └─ execution ticket ID
```

The protocol does not make every worker implementation identical. It makes
lease, retry, authority, artifact and observability semantics identical while
leaving scientific behavior in the owning engine.

## IPC

Control messages are bounded to at most 1 MiB by the Rust IPC foundation and
are expected to be much smaller in normal operation. Large payloads use a
`BufferDescriptor` carrying segment identity, generation, range, element/shape
metadata, content digest, owner, lifetime lease, access mode, transport and
locator. The runtime host validates descriptors before exposing them to a
worker. Protobuf is never used as the bulk tensor/dataset transport.
