# Mindclade system design reference

**Status:** Canonical architecture reference  
**Audience:** platform, infrastructure, ML systems, data, model, security, and product engineers  
**Last consolidated:** 2026-08-13  
**Authority:** accepted ADRs under `docs/design/`, machine-readable repository policies, and qualification evidence  

This document consolidates the system design developed for the Mindclade internal
monorepo. It is intentionally broader than any individual service or package.
Detailed chapters remain authoritative for local implementation details; this
reference defines how those pieces fit together, where authority lives, and what
must remain true as the system evolves.

A repository path existing does not imply production readiness. Source
implementation, qualification, deployment readiness, and production promotion
are distinct states. See `components.toml`, `maturity.toml`, `SCAFFOLD_STATUS.md`,
`VALIDATION.md`, and `QUALIFICATION.md`.

---

## 1. System mission

The platform supports the full lifecycle of frontier biological AI systems:

```text
external scientific data
  -> ingestion and immutable source snapshots
  -> curation and quality control
  -> scientific preprocessing and feature generation
  -> versioned datasets and reference databases
  -> model training and distributed checkpointing
  -> independent evaluation and safety qualification
  -> model/runtime release evidence
  -> batch and online inference
  -> artifacts, lineage, audit, usage, and product surfaces
```

The architecture must scale from a small startup team to multi-cluster,
multi-model operation without forcing premature microservice fragmentation or
allowing scientific/numerical semantics to leak into fleet infrastructure.

### 1.1 Primary design goals

- deterministic, reproducible model and data execution;
- durable workflows for work that can outlive a process or node;
- low-latency online inference without a synchronous control-plane dependency;
- explicit ownership across Go, Rust, Python, TileLang, and TypeScript;
- content-addressed immutable data, models, checkpoints, and evidence;
- fail-closed security and release behavior;
- bounded memory, queues, retries, I/O, subprocesses, and shutdown;
- strong numerical and cross-language qualification;
- one build graph and one toolchain authority;
- modular boundaries that can later become services when measured need exists.

### 1.2 Non-goals

The architecture deliberately does not optimize for:

- one implementation language for the entire company;
- one microservice per domain directory;
- generic abstractions without multiple real consumers;
- synchronous policy lookups on every inference request;
- mutable artifact names as identity;
- unbounded queues or implicit retries;
- hidden provider SDKs inside domain logic;
- duplicating model semantics in Go or Rust;
- treating scaffold completeness as product completeness.

---

## 2. Architectural laws

These laws have priority over local convenience.

### 2.1 Language authority

```text
Go         = fleet control plane and durable global policy
Rust       = online/runtime data plane and node execution
Python     = scientific, model, training, inference, and evaluation numerics
TileLang   = qualified accelerator kernels
TypeScript = product surfaces and browser/public web clients
```

Authority is semantic, not merely syntactic. A Rust process may enforce a
model-independent resource budget, but it must not define what makes two
PyTorch tensor batches compatible. Python may invoke an optimized kernel, but it
must not promote an unqualified implementation. Go may publish route policy,
but it must not remain on the hot path of an already-authorized online request.

### 2.2 Repository ownership

```text
protocols/      canonical cross-process/wire contracts
libs/           stable reusable mechanisms by language
control/        reusable Go control-plane domain policy and durable state
services/       deployable composition roots only
data/           source, curation, dataset, loader, and scientific data semantics
preprocessing/  model-input scientific preparation and feature semantics
models/         model architecture, parameter, output, and model-family contracts
training/       trainer, distributed execution, optimizers, checkpoint semantics
evaluation/     independent qualification harnesses and gates
serving/        reusable Python/Rust inference implementation libraries
kernels/        provider system and qualified TileLang operations
apps/           product surfaces consuming SDKs/contracts
infra/          deployment infrastructure, Kubernetes, security, observability
ci/, tools/     build, codegen, qualification, release, and developer workflows
```

### 2.3 Mechanisms, policy, and processes

The Go architecture follows a strict three-level split:

```text
libs/go     reusable mechanisms
    ↓
control/    business/domain policy and durable state machines
    ↓
services/   process composition, providers, transports, deployment lifecycle
```

Examples:

```text
libs/go/coordination/outbox
    generic transactional publication mechanism

control/registry
    model/dataset/reference/release policy

services/control_plane
    database, broker, API, and lifecycle composition
```

The equivalent Rust rule is:

```text
libs/rust        reusable node/runtime mechanisms
services/*       concrete runtime processes
Python engines   scientific/numerical behavior invoked by those processes
```

### 2.4 Dependency direction

The intended high-level graph is:

```text
protocols
   ↓
foundational libs
   ↓
{control, data, preprocessing, models, kernels}
   ↓
{training, serving, evaluation}
   ↓
services
   ↓
apps through generated SDKs/contracts only
```

Additional rules:

```text
research -> production packages          allowed
production -> research                    forbidden
service -> app                            forbidden
app -> service internals                  forbidden
model family -> unrelated model family   forbidden
libs -> control/services                  forbidden
provider-neutral -> provider adapter      forbidden except through interface
```

Machine-readable dependency budgets enforce important portions of this graph.

---

## 3. Logical planes

The complete platform is easier to reason about as six cooperating planes.

```text
┌──────────────────────────────────────────────────────────────────────┐
│ Product / API plane                                                  │
│ apps · SDKs · public APIs · admin/console                            │
└──────────────────────────────┬───────────────────────────────────────┘
                               │
┌──────────────────────────────▼───────────────────────────────────────┐
│ Go durable control plane                                             │
│ tenancy · runs · jobs · registry · scheduling · routing · usage      │
│ ingestion control · release policy · audit · runtime authority       │
└──────────────┬───────────────────────────┬───────────────────────────┘
               │                           │ signed bounded authority
               │ durable stages            ▼
               │                 ┌─────────────────────────────────────┐
               │                 │ Rust runtime/node data plane        │
               │                 │ gateway · host · node agent · CAS   │
               │                 │ local admission · byte movement     │
               │                 └────────────────┬────────────────────┘
               │                                  │
               ▼                                  ▼
┌──────────────────────────────────────────────────────────────────────┐
│ Python scientific/numerical plane                                   │
│ curation · preprocessing · models · training · eval · tensor batch   │
└──────────────────────────────┬───────────────────────────────────────┘
                               │
┌──────────────────────────────▼───────────────────────────────────────┐
│ Accelerator plane                                                    │
│ PyTorch references · TileLang qualified implementations · vendor     │
└──────────────────────────────┬───────────────────────────────────────┘
                               │
┌──────────────────────────────▼───────────────────────────────────────┐
│ Evidence / artifact plane                                            │
│ immutable artifacts · checkpoints · datasets · reference releases    │
│ provenance · SBOM · evaluation · release evidence                    │
└──────────────────────────────────────────────────────────────────────┘
```

---

## 4. Canonical identities and immutable references

### 4.1 Resource identifiers

Internal resources use stable typed identifiers with canonical parsing and
cross-language golden vectors. Identifiers are not database primary-key
implementation details and must not be silently re-encoded at service
boundaries.

### 4.2 Resource versions

Mutable control-plane resources use content-bound or generation-bound resource
versions for optimistic concurrency. A write that declares a precondition must
fail if the current resource version differs.

Transport mappings may use:

```text
HTTP       ETag / If-Match / If-None-Match
Protobuf   resource_version + precondition
Events     aggregate/resource version
SQL        conditional UPDATE on version
```

Provider-specific object-store generations remain provider metadata and are not
confused with control-plane resource versions.

### 4.3 Artifact identity

Artifact identity is content plus contract, not location:

```text
ArtifactRef
  digest
  size_bytes
  media_type
  logical_kind
  schema_version

ArtifactLocation
  artifact_digest
  provider
  URI
  provider_generation
  region (optional)
```

The same `ArtifactRef` concept is used for:

- raw source archives;
- canonical scientific records;
- dataset shards;
- MSA and template-search outputs;
- feature bundles;
- checkpoint shards and manifests;
- model/runtime bundles;
- evaluation outputs;
- reference-database shards;
- build, provenance, SBOM, and release evidence.

Moving, replicating, or caching bytes changes locations, never identity.

---

## 5. Protocol authority

The repository uses several interface technologies with explicit ownership.

| Surface | Canonical source |
|---|---|
| Internal RPC and runtime control | Protobuf |
| Durable internal event payloads | versioned Protobuf/event contract |
| Public REST API | OpenAPI |
| Event catalog/documentation | generated AsyncAPI/JSON projections where required |
| Runtime signed claim bytes | canonical MCCE1 signing representation |
| Browser/SDK language types | generated from canonical contracts |
| Resolved configuration | canonical schema-validated document + digest |

One concept may appear in several surfaces, but fields either have one source of
truth or a tested explicit mapping.

### 5.1 Compatibility requirements

Compatibility checks cover:

- removed or reused fields;
- enum evolution;
- required-field changes;
- identifier/timestamp representation;
- pagination semantics;
- resource-version behavior;
- error mapping;
- event replay;
- unknown-field preservation where applicable;
- cross-language signing bytes and golden vectors.

---

## 6. Go foundation and durable coordination

`libs/go` is intentionally a stable mechanism layer, not a home for control
business logic. Admission of new generic packages requires multiple independent
production consumers, domain-neutral semantics, tests, documentation, ownership,
and layer review.

### 6.1 Layer model

```text
Layer 0  foundations
  clock · faults · identifiers

Layer 1  shared contracts
  audit · auth · idempotency · requestmeta · pagination · resourceversion

Layer 2  runtime mechanisms
  config · messaging · observability · retry · servicekit · signing
  coordination contracts

Layer 3  infrastructure adapters
  PostgreSQL · Redis · GCS · Kubernetes · provider-backed coordination

Layer 4  transport adapters
  HTTP · Connect · gRPC

Layer 5  consumers outside libs/go
  control/ · services/ · operators/controllers/workers
```

### 6.2 Transactional outbox

Canonical durable mutation:

```text
authenticate / authorize
  -> validate idempotency key and fingerprint
  -> begin SQL transaction
      -> apply domain mutation with resource-version precondition
      -> append audit record
      -> append outbox event
  -> commit
  -> respond
```

Publishing happens after commit. A request transaction never directly relies on
successful broker delivery.

### 6.3 Transactional inbox and projection

```text
broker delivery
  -> begin transaction
      -> claim/deduplicate inbox record
      -> apply projection/domain effects
      -> compare-and-advance monotonic cursor
      -> optionally append downstream outbox event
  -> commit
  -> acknowledge delivery
```

Receiving the same event ID with a different payload digest is an integrity
failure, not a normal duplicate.

### 6.4 Fenced work queues

Long-running Go-owned control work uses:

```text
claim
  -> fencing token
  -> heartbeat / lease renewal
  -> claim-loss cancellation
  -> complete | retry | dead-letter
```

Stale workers cannot complete work after a newer claim is issued.

### 6.5 Leadership

Singleton authority is implemented with the same fencing/lease semantics.
Leadership is a mechanism; the reason a domain requires singleton authority
remains domain policy.

### 6.6 `servicekit` lifecycle

Every Go daemon, controller, operator, dispatcher, and durable worker uses the
same lifecycle substrate:

```text
Created -> Starting -> Running -> Draining -> Stopping -> Stopped
```

Drain semantics:

1. readiness fails immediately;
2. new work stops being admitted/claimed;
3. owned tasks receive cancellation/drain signals;
4. a bounded completion window is honored;
5. remaining leases/work are released safely;
6. components stop in reverse dependency order;
7. telemetry flush is bounded.

Long-lived goroutines belong to named owned task groups. Detached production
background tasks are forbidden.

---

## 7. Go control plane

The control plane owns durable global policy and fleet state. It is designed as
a modular monolith in code/data semantics, with independently runnable process
roles where operationally useful.

### 7.1 Primary domains

```text
control/
  admission          entitlement/quota admission
  artifacts          artifact catalog and access policy
  audit              audit policy/export
  evaluations        qualification and gate records
  events             event publication/mapping policy
  ingestion          source snapshot and stage control
  lineage            provenance graph
  metadata           run/build/metric metadata
  orchestration      jobs, workflows, stage attempts
  registry           datasets/models/checkpoints/reference/releases
  routing            deployment and route-snapshot policy
  runs               run/job/attempt lifecycle
  runtime_authority  grants, tickets, revocation epochs
  scheduling         global quotas, fairness, placement, reservations
  tenancy            orgs/projects/identities/entitlements
  usage              metering and quota reconciliation
  webhooks            subscriptions and delivery policy
  weights             sensitive model-weight access
```

### 7.2 Control-plane process roles

The same codebase may expose different process profiles:

- API;
- scheduler;
- controller;
- Kubernetes operator;
- ingestion coordinator;
- event projector;
- event dispatcher;
- webhook dispatcher;
- registry process;
- administration/maintenance process.

Each profile declares exact required capabilities and fails startup if a
required production provider is absent.

### 7.3 Why Go is not the online data plane

Go owns global route and entitlement policy but does not sit synchronously in
front of every accepted inference request. Instead it publishes immutable
snapshots and bounded signed grants that Rust can verify locally.

This isolates control-plane latency/outages from already-authorized online work
and keeps policy ownership centralized.

---

## 8. Runtime authority

Runtime authority is the cross-language boundary between durable Go policy and
local Rust execution.

### 8.1 Route snapshot

A signed immutable route snapshot contains the deployment state needed for
local routing, including:

```text
snapshot_id / digest
policy_epoch
created_at / expires_at
model/deployment identities
bundle digests
capability constraints
routing weights / canary assignments
regional restrictions
runtime compatibility
safety requirements
```

Snapshots are monotonic. Rust rejects rollback to an older snapshot unless an
explicit recovery protocol authorizes it.

### 8.2 Admission grant

A short-lived online grant is bounded by tenant, capability, region,
concurrency, request count, compute/input/output budgets, validity period,
policy epoch, and signature.

A grant is normally session- or budget-scoped rather than requiring a
synchronous Go mint operation for every request.

### 8.3 Execution ticket

Durable stages use a ticket containing:

```text
ticket_id
issuer
tenant/workspace
run/job/stage/attempt identity
fencing token
model bundle digest
runtime/engine bundle digest
resolved configuration digest
allowed artifact inputs/outputs
reference snapshot digests where applicable
CPU/RAM/GPU/disk/network/output budgets
deadline
policy epoch
route epoch where applicable
revocation epoch
issued-at / not-before / expiration
signature algorithm + key ID + signature
```

### 8.4 Revocation

A high-priority revocation snapshot/epoch can invalidate cached authorization
before ordinary ticket expiry. Runtime admission requires both the ticket and
local revocation state to be sufficiently fresh.

### 8.5 Canonical signing bytes

Go and Rust do not sign private in-memory struct encodings. MCCE1 defines stable
canonical signing bytes; Protobuf remains the canonical wire/control message.

---

## 9. Control-plane outage semantics

A temporary Go control-plane outage has defined behavior:

| Situation | Behavior |
|---|---|
| Already-admitted work | continues within ticket/deadline unless revoked |
| Valid unexpired grant | may admit within remaining local budget while snapshots remain fresh |
| New work without authority | rejected |
| Expired route snapshot | new admission fails; existing work drains according to policy |
| Stale revocation state | fail closed for new admission |
| Usage sink unavailable | append to bounded durable local spool |
| Usage spool full | reject new work instead of creating unaccounted usage |
| Emergency release withdrawal | newer revocation epoch invalidates affected cached authority |

This behavior must be tested as a runtime invariant, not merely documented.

---

## 10. Rust runtime foundation

Rust owns the latency-sensitive, resource-sensitive, and byte-sensitive
execution environment.

### 10.1 Cohesive crate design

The runtime is intentionally deep rather than fragmented into dozens of tiny
crates. Major areas include:

```text
runtime_core       cancellation, deadlines, retry, fencing, hierarchical budgets
bytes_io           bounded buffers, ranges, vectored/copy accounting
content_digest     digest calculation/verification
bounded_parse      allocation/record/nesting/input limits
bio_formats        bounded scientific format parsing
manifests          artifact/checkpoint/runtime manifest primitives
object_store       provider-neutral bounded object access
artifact_cas       content-addressed storage mechanisms
checkpoint_io      checkpoint staging/verification/repair/transfer
data_stream        bounded prefetch, ranges, resume, streaming
ipc                bounded control framing and bulk descriptors
worker_protocol    ticket/command/status validation
worker_runtime     explicit worker state machine and commit semantics
servicekit         Rust process lifecycle/task supervision
telemetry          structured metrics/tracing/logging adapters
telemetry_spool    bounded durable diagnostics/telemetry buffering
gpu_host           GPU slot/resource accounting
python_bridge      narrow leaf bindings for small in-process primitives
```

### 10.2 Runtime stack

Production Rust standardizes on one async stack:

```text
Tokio          async runtime, I/O, timers, channels
Tonic/Prost    generated gRPC/Protobuf control interfaces
Tower          timeouts, concurrency, rate limits, load shedding
Bytes          reusable network/record buffers
Tracing/OTel   structured telemetry through adapters
```

No nested Tokio runtimes, unowned spawned tasks, or unbounded production queues
are allowed.

### 10.3 Node-wide resource budget

Resource accounting is hierarchical:

```text
node
  ├─ service
  │   ├─ worker
  │   │   ├─ request
  │   │   └─ operation
  │   └─ background tasks
  └─ shared caches
```

Tracked resources include:

- resident and pinned host memory;
- shared memory;
- buffer-pool bytes;
- local disk;
- file descriptors;
- in-flight object-store/network requests;
- queued requests;
- active processes and CPU worker threads;
- GPU allocation estimate;
- checkpoint staging bytes;
- reference/artifact cache bytes;
- telemetry spool bytes.

Substantial allocation or transfer acquires a reservation first. Reservations
release through RAII/structured ownership.

---

## 11. Runtime gateway

The Rust runtime gateway is the network face of online inference.

Responsibilities:

```text
authentication boundary / principal binding
signed admission-grant validation
route-snapshot and revocation cache
local route selection
request framing and bounds
local concurrency / request / compute budget
load shedding and backpressure
cancellation and deadlines
SSE/streaming response multiplexing
usage accounting/spooling
health, readiness, drain
```

It does **not** own model tensor compatibility, model architecture, scientific
input semantics, or global deployment policy.

---

## 12. Runtime host

The Rust runtime host owns the process boundary around Python model workers.

Responsibilities:

```text
execution-ticket revalidation
model/runtime bundle compatibility
Python process supervision
model-slot lifecycle
host/GPU resource reservations
coarse compatibility grouping
bounded control IPC
bulk-data descriptor validation
cancellation and drain
restart/recovery policy
local artifact/reference cache access
result commit fencing
```

It does not embed Python as the primary long-lived model runtime and does not
own final tensor batching.

---

## 13. Rust/Python batching boundary

Rust performs model-independent admission and coarse grouping. Python owns the
final numerical batch.

Rust may group on declared manifest properties such as:

```text
model/deployment
execution class
broad token/atom envelope
precision/runtime capability
requested output class
```

Python decides:

```text
exact tensor compatibility
padding/packing
shape buckets
KV/cache layout
compilation or CUDA-graph bucket
MSA/template/atom tensor geometry
sampling/diffusion schedule compatibility
actual device-memory allocation
```

The runtime may use a request/plan handshake in which Rust proposes admitted
request envelopes and Python returns an executable `BatchPlan` plus memory
estimate.

---

## 14. IPC and bulk data

Control and bulk payloads use separate paths.

### 14.1 Control path

```text
Protobuf over gRPC or Unix-domain socket
bounded messages
commands
status
heartbeats
deadlines
cancellation
buffer descriptors
```

### 14.2 Bulk path

Large data uses one of:

- shared memory;
- memfd/file descriptor;
- local immutable file;
- memory-mapped segment;
- content-addressed object reference.

A `BufferDescriptor` records:

```text
segment_id
generation
offset / length
element or record type
shape/stride metadata
content digest
owner/lifetime lease
access mode
transport/locator
```

Large tensors, dataset batches, MSA matrices, or checkpoint chunks are not
encoded directly into Protobuf control messages.

---

## 15. Unified durable stage execution

Ingestion, preprocessing, dataset builds, evaluation, batch inference, training
support work, checkpoint transfer, rollout, and simulation share one durable
stage vocabulary.

### 15.1 Stage specification

```text
StageSpec
  stage_id
  stage_kind / operation
  immutable input ArtifactRefs
  output namespace
  resolved config digest
  optional reference snapshot digests
  resource budget
  timeout / deadline
  maximum attempts
  dependency stage IDs
```

### 15.2 Stage attempt

```text
StageAttempt
  run/job ID
  stage ID
  attempt number
  fencing token
  execution ticket ID
  assigned execution class/node class
```

Go owns DAG state, retry eligibility, quotas, durable attempts, and terminal
state. Rust owns bounded node execution. Python owns scientific/numerical stage
behavior when required.

---

## 16. Artifact and storage plane

The artifact plane is optimized for immutable, tenant-scoped byte movement.

### 16.1 Write protocol

```text
allocate scoped upload
  -> stream to bounded staging
  -> compute and verify digest/size
  -> publish immutable content-addressed object
  -> atomically commit manifest/catalog reference
  -> emit lineage/audit/event evidence
```

A failed or timed-out upload may leave staging bytes, but it cannot create a
valid committed artifact without the manifest/commit boundary.

### 16.2 Reads

Reads enforce tenant scope, operation grants, byte ranges, deadlines, and digest
verification policy. Signed URLs are short-lived capabilities, not identities.

### 16.3 Cache behavior

Corrupt cache entries are quarantined and refetched. Durable object corruption
blocks dependent work and emits repair evidence. Repair never rewrites different
bytes under an existing digest.

### 16.4 Garbage collection

GC considers reachability, active leases, legal/retention holds, checkpoint
ancestry, release references, replication state, and a safety delay. Deletion
is idempotent and audited.

---

## 17. Data ingestion

Ingestion is a durable pipeline, not a request handler.

### 17.1 End-to-end flow

```text
external source
  -> Go discovers/records immutable source snapshot
  -> Go compiles durable stages and execution tickets
  -> Rust worker fetches/resumes/validates bytes
  -> bounded parser frames source records
  -> raw ArtifactRefs committed
  -> Python canonicalizes scientific records
  -> Python quality/curation/safety/license transforms
  -> deterministic dataset shards built
  -> Go registry publishes immutable dataset version + lineage
```

### 17.2 Source layers

The data factory distinguishes:

```text
raw
canonical
curated
model-ready
```

Each layer is immutable and reproducible from recorded inputs, code, config,
and toolchain/reference versions.

### 17.3 Connector ownership

Go owns durable source cursor/snapshot state. Rust owns high-throughput fetch,
range reads, decompression, bounded parsing, and local staging. Python owns
scientific normalization and curation semantics.

---

## 18. Scientific preprocessing

Preprocessing is a first-class domain because biological models often require
long-running CPU/high-memory preparation before GPU inference.

Primary areas:

```text
contracts
pipeline / DAG
entity canonicalization
deduplication
MSA search and parsing
template search and selection
ligand / CCD preparation
chemistry normalization
feature construction
cache keys
reference-data binding
provenance
```

The same architecture supports structure prediction, embedding pipelines,
annotation, multimodal preparation, evaluation preprocessing, and future
scientific tasks.

---

## 19. NovaFold-style MSA and template pipeline

Canonical structure-model preparation:

```text
1. validate and canonicalize entities
2. deduplicate identical chains/entities
3. lookup per-entity feature/search caches
4. protein and/or RNA MSA search
5. construct sequence/profile representation
6. template search against immutable snapshot
7. apply release-date cutoff and filter/deduplicate hits
8. retrieve/validate template structures
9. build paired MSA for complexes
10. prepare ligands/CCD/covalent modifications
11. construct model-specific token/atom/pair/template features
12. commit PreprocessedInputBundle
13. admit GPU inference only after bundle verification
```

Template search is explicitly dependent on the relevant MSA/profile when the
search method requires it.

### 19.1 GPU reservation rule

No inference GPU or model slot is reserved while MSA/template/reference search
is pending. CPU/high-memory search and GPU model execution are separately
scheduled resource classes.

---

## 20. Reference database releases

Reference databases are promoted immutable releases, not mutable paths named
“latest.”

A release records:

```text
snapshot_id / digest
database type
upstream/source versions
release/cutoff date
shard ArtifactRefs
index format/version
generating tool/version
compatible search tool versions
license/provenance
promotion state
retention policy
```

Rust node agents stage exact requested snapshots to read-only local cache/NVMe
and verify digests before activation. Python records the snapshot digest in
scientific provenance.

A reproducible structure prediction is therefore bound to:

```text
model bundle digest
+ preprocessing resolved-config digest
+ reference DB snapshot digest(s)
+ search-tool/version provenance
+ scientific input digest
```

---

## 21. Preprocessing cache model

Caches are content-derived and policy-versioned.

### 21.1 MSA cache key

```text
normalized sequence digest
+ entity type
+ search protocol digest
+ search tool/version
+ reference snapshot digests
+ search parameters digest
```

### 21.2 Template cache key

```text
sequence/profile digest
+ template snapshot digest
+ maximum template date
+ search/filter/selection policy digests
+ tool/version
```

### 21.3 Paired-MSA cache key

```text
ordered entity digests
+ per-entity MSA digests
+ pairing policy digest
+ crop policy digest
```

### 21.4 Feature-bundle cache key

```text
canonical complex digest
+ MSA/template/ligand artifact digests
+ feature schema version
+ model input contract version
+ featurization policy digest
```

Raw search results, parsed alignments, filtered alignments, template hits,
selected templates, and final feature bundles are separate immutable artifacts
so downstream policy changes do not force expensive search repetition.

---

## 22. Dataset publication

A dataset version is a release artifact, not a mutable collection of files.

Publication binds:

```text
dataset ID/version
manifest digest
shard ArtifactRefs
schema/tokenizer/feature versions
source snapshot lineage
curation policy/config digest
quality/safety/license evidence
statistics
hidden/evaluation-set controls where applicable
```

Training and evaluation consume exact dataset versions or mixtures with
resolved digests.

---

## 23. Model architecture ownership

Models own architecture and parameter semantics, not distributed infrastructure.

Model contracts include:

```text
configuration
input/output schemas
parameter tags/state names
initialization
parallel-plan declarations
compile-plan declarations
checkpoint compatibility
export/serving adapters
```

Model families must not import one another merely for convenience. Shared
scientific/model components belong in explicit common component packages.

Hugging Face or other external ecosystem compatibility lives in adapters rather
than defining native model structure.

---

## 24. Training architecture

Python owns the authoritative trainer, state machine, task/objective semantics,
optimizer construction context, numerical execution, and checkpoint state
registration.

### 24.1 Training structure

```text
training/contracts
  task · batch · state · step · engine · optimizer · checkpoint · evaluation

training/core
  trainer · loop · state registry · lifecycle · policies

training/engines
  native · TorchTitan · Fabric

training/distributed
  mesh · groups · parallel plans · collectives · pipeline · MoE

training/checkpointing
  DCP orchestration · planners · atomic commit · reshard · resume

training/runtime
  compilation · precision · memory · resilience · telemetry · hooks/callbacks

training/tasks
  LM · diffusion · biology · multimodal · reinforcement · preference
```

There is exactly one authoritative `Trainer`, `TrainingState`, step lifecycle,
and checkpoint state registry.

### 24.2 Engine adapters

- **native** is the PyTorch-native reference/production execution path;
- **TorchTitan** consumes Mindclade task/state/topology/checkpoint contracts as a
  large-scale execution adapter;
- **Fabric** is a developer/runtime adapter and telemetry integration, not an
  independent distributed control plane layered around TorchTitan.

---

## 25. Hooks and callbacks

The distinction is semantic and enforced:

```text
Hooks
  synchronous
  ordered
  rank/execution-scope aware
  may mutate numerical behavior
  may participate in critical path

Callbacks / observers
  non-mutating consumers
  normally asynchronous
  bounded/backpressured/retryable
  cannot initiate distributed collectives
```

This avoids a second hidden execution model inside the trainer.

---

## 26. Distributed training

The model declares a parallel plan; training compiles it into runtime topology.

Supported dimensions include:

- data parallel / DDP;
- FSDP2;
- tensor parallel;
- pipeline parallel;
- context/sequence parallel;
- expert parallel;
- hybrid multi-dimensional plans.

End-to-end authority:

```text
Go scheduler/Kueue
  admits quota and placement class

Kubernetes JobSet
  materializes coordinated worker Pods

Rust node agent
  stages artifacts, transfers checkpoints, monitors/preempts locally

Python distributed runtime
  initializes process groups/meshes and executes numerical collectives
```

Collective schedules, topology fingerprints, reduction groups, and layout
metadata are recorded for checkpoint/reproducibility qualification.

---

## 27. Checkpoint authority

Checkpoint concerns are split narrowly:

| Layer | Authority |
|---|---|
| model family | model-specific state names and transformations |
| `training/checkpointing` | registered state, save/load plans, DCP, reshard/resume |
| Rust `checkpoint_io` | staging, byte transfer, digest verification, repair |
| artifact plane | durable tenant-scoped storage and retention |
| protocols | manifest and artifact-reference schemas |

### 27.1 Atomic commit

```text
safe point
  -> distributed save plan
  -> write content-addressed shards to staging
  -> verify digest and metadata
  -> write complete manifest
  -> atomically publish commit record
  -> update catalog/retention asynchronously
```

Only a committed manifest is restorable.

### 27.2 Registered state

The registry supports model, optimizer, scheduler, scaler, EMA, RNG, data-loader
position, task state, curriculum, sampler, rollout/replay state, and arbitrary
checkpointable components.

### 27.3 Async staging

Host-memory and transfer staging consume explicit Rust node budget. Async
checkpointing is never assumed to be free.

---

## 28. Evaluation architecture

Evaluation is independent of training and may target:

- a checkpoint;
- a model bundle;
- a runtime bundle;
- a serving endpoint;
- a release candidate;
- an external submission.

Evaluation owns harness isolation, suites, metrics, robustness, privacy,
biological-risk, safety, regression, external import, and reporting.

Every result records exact subject, dataset, suite/task/metric versions, config,
seed/sampling plan, device/environment, kernel providers, tolerances, raw
artifacts, aggregation, and gate decision.

Hidden/safety datasets use scoped identities/tickets and are not enumerable by
model code.

---

## 29. Release evidence graph

Release promotion is graph-native.

```text
Model/Runtime Release
  ├─ source commit/build evidence
  ├─ training run
  │   ├─ resolved config
  │   ├─ dataset versions
  │   ├─ reference snapshots
  │   ├─ toolchain/environment
  │   └─ checkpoints
  ├─ model bundle
  ├─ runtime bundle
  ├─ kernel qualification
  ├─ numerical qualification
  ├─ performance qualification
  ├─ evaluation and safety evidence
  ├─ SBOM/provenance
  ├─ security/weight-access evidence
  ├─ rollback evidence
  └─ signatures
```

The registry validates required evidence kinds, acyclicity, subject binding,
policy epoch, deterministic graph digest, and current promotion policy. It does
not fabricate evidence.

---

## 30. Online inference

Canonical request flow:

```text
client
  -> Rust runtime gateway
      verify principal/grant
      check route/revocation freshness
      locally resolve route
      reserve request/concurrency budget
      frame request and deadline
  -> Rust runtime host
      verify execution authority
      reserve host/GPU/model slot
      supervise Python worker
  -> Python/PyTorch model worker
      final tensor batch
      model execution
      sampling/diffusion/confidence
      qualified kernels
  -> Rust runtime host/gateway
      multiplex bounded streaming result
      account/release resources
      emit usage/diagnostics
```

Go is not a synchronous dependency after authorization has been materialized as
valid local authority.

---

## 31. Batch inference and durable prediction

Long-running or preprocessing-heavy inference uses the durable stage system:

```text
Go run submission
  -> durable preprocessing stages
  -> immutable prepared input bundle
  -> batch GPU execution stage
  -> confidence/ranking/postprocessing
  -> terminal artifacts and run state
```

This is the default for NovaFold-style full pipelines. A separate prepared-input
API can bypass preprocessing when the caller already has a valid feature bundle.

---

## 32. Kernel provider system

PyTorch is the semantic reference. TileLang and vendor kernels are optional
qualified providers.

A kernel may become the default only for a qualified signature such as:

```text
operation
+ dtype
+ shape family
+ layout
+ device architecture
+ compiler/runtime version
+ numerical tolerance class
```

Unknown or revoked signatures fall back. Runtime requests do not promote
schedules. Autotuning is offline and produces immutable evidence.

Qualification covers:

- forward/backward numerical parity;
- gradients;
- aliasing/noncontiguous layouts;
- ragged/boundary shapes;
- NaN/Inf behavior;
- compile/fake-tensor/custom-op integration;
- throughput/latency;
- deterministic promotion/revocation.

---

## 33. Configuration system

Configuration is composable but resolves to one canonical document.

```text
base
+ model preset
+ task
+ data profile
+ hardware profile
+ parallelism profile
+ precision profile
+ kernel policy
+ environment
+ explicit overrides
= ResolvedConfig
```

The resolver:

- rejects unknown keys;
- validates compatibility;
- records source/provenance for every override;
- distinguishes absent from explicitly empty values;
- redacts secrets;
- produces deterministic canonical serialization and SHA-256 digest.

Runs, checkpoints, evaluations, incidents, and releases reference the resolved
config digest rather than an ambiguous stack of source files.

---

## 34. Scheduling and resource classes

The Go scheduler owns global admission, quota, fairness, reservations, and
placement policy. Kueue/JobSet provide Kubernetes-native admission/materialized
workload mechanics.

Separate resource classes/pools include:

| Pool | Typical work |
|---|---|
| search CPU/high-memory | MSA/template/reference search |
| featurization CPU | pairing, parsing, feature construction |
| chemistry CPU | CCD/conformer/chemistry work |
| GPU inference | online/batch model execution |
| GPU training | multinode training |
| evaluation | isolated CPU/GPU qualification |
| artifact/data plane | transfer/cache/checkpoint traffic |

Scheduling must not hold an expensive downstream resource while an upstream
resource-class stage is incomplete.

---

## 35. Node agent

The Rust node agent is shared infrastructure for training, preprocessing,
ingestion, and transfer hot paths rather than multiple unrelated daemons.

Capabilities include:

```text
reference database cache
artifact cache
checkpoint transfer
streaming/prefetch
external scientific subprocess supervision
CPU/RAM/disk/network limits
resource telemetry
workspace/temp cleanup
artifact transfer and verification
preemption/drain integration
```

Scientific algorithms remain in Python or external hermetic binaries.

---

## 36. Scientific parsers

Untrusted scientific byte parsing uses bounded Rust parsers where performance or
safety justifies it.

Every parser declares:

```text
strict mode
curation/recovery mode
max input bytes
max line length
max records
max tokens/atoms
max nesting
max metadata entries
max total allocation
byte-offset diagnostics
canonical serializer
```

High-risk parsers require malformed corpora, round-trip tests, truncation tests,
fuzz targets, and allocation-limit tests.

---

## 37. Object store abstraction

Rust storage uses one provider-neutral object-store mechanism wrapped with
Mindclade semantics:

```text
tenant namespace enforcement
artifact grant enforcement
conditional operations
range validation
content-digest verification
bounded retry/deadline classification
multipart transfer
metrics
content-addressed naming
atomic manifest publication
encryption hooks
```

The platform does not reimplement a different cloud abstraction per subsystem.

---

## 38. Observability

Telemetry is structured and bounded across languages.

Common semantic dimensions include:

```text
tenant / workspace
run / job / stage / attempt
request / correlation / causation
resource kind / operation
outcome / fault code / reason
queue / delivery attempt / lease owner
policy / route / revocation epoch
model/runtime release
reference snapshot
kernel provider
```

Rules:

- secrets and signed URLs are never logged;
- arbitrary model/data payloads are not telemetry;
- metric label cardinality is bounded;
- local telemetry spools are durable but bounded;
- full spools cause fail-closed admission where accounting/security requires it;
- health of telemetry delivery itself is observable.

---

## 39. Security and trust boundaries

Major trust boundaries:

```text
public client -> edge/runtime gateway
runtime gateway -> runtime host
runtime host -> Python model process
worker -> artifact/reference store
Go control plane -> signed runtime authority
hidden evaluation -> isolated evaluation worker
model-weight catalog -> approved runtime/training identity
CI/build -> signed release artifact
```

Security principles:

- short-lived scoped grants rather than long-lived broad credentials;
- workload identity instead of embedded secrets;
- tenant scope enforced at storage and runtime boundaries;
- revocation epochs for urgent invalidation;
- immutable audit for sensitive policy/weight operations;
- break-glass is explicit, time-bounded, reasoned, and audited;
- model weights and hidden sets require stronger authorization and environment
  qualification than ordinary artifacts;
- production images and releases require digest pinning, SBOM, provenance, and
  signature evidence.

---

## 40. Build and toolchain system

The build contract is intentionally strict.

### Bazel owns

```text
build
test
code generation
packaging
OCI application images
qualification targets
release bundles
SBOM/provenance attachment
```

### Nix owns

```text
compilers/interpreters/SDKs
system packages
developer shells
toolchain bundles
remote execution base images
```

Rules:

- Bzlmod is the Bazel dependency mechanism;
- one pinned Nix derivation must define equivalent local and remote toolchains;
- Bazel actions may not discover undeclared host tools;
- production actions do not run package-manager installs;
- Nix cache, Bazel action cache, and platform artifact CAS are separate systems;
- final production OCI images are Bazel release outputs consuming immutable Nix
  bases.

---

## 41. Dependency ecosystems

Internal monorepo policy prefers one root dependency graph per language:

```text
Go      /go.mod + /go.sum
Rust    workspace Cargo.toml + Cargo.lock
Python  pyproject.toml + uv.lock
TS      pnpm workspace + lockfile
```

Independent public SDKs or separately published external projects may be an
explicit exception.

---

## 42. Component maturity and scaffold activation

A path can exist before it is active. Machine-readable statuses include:

```text
planned
scaffolded
experimental
implemented
qualified
production
deprecated
```

Policy:

```text
planned/scaffolded
  cannot be a production dependency

experimental
  cannot enter release graph without explicit exception

implemented
  requires meaningful tests and ownership

qualified
  requires recorded qualification evidence

production
  requires owner, SLO/operational expectations, runbook, release target,
  security/dependency review, and rollback path
```

This allows a comprehensive target-state monorepo without pretending every
materialized file is active architecture.

---

## 43. Service decomposition policy

A module becomes an independently deployed service only when evidence justifies
it. Triggers include:

- independently scaling load;
- materially different availability/SLO requirements;
- security or tenant-isolation boundary;
- independent operational ownership;
- independent release cadence;
- proven persistence contention or failure-domain requirement.

Absent those triggers, durable Go policy remains a modular control plane and
reusable Python/Rust logic remains libraries invoked by thin process roots.

---

## 44. Failure model

Failures are classified by ownership and retryability rather than by transport
alone.

### 44.1 Typical classes

```text
invalid input / scientific validation       terminal until input/policy changes
transient provider/network                  retry with bounded policy
resource exhaustion                         backpressure/reschedule, not blind retry
stale fence/resource version                terminal for current attempt
control-plane unavailable                   bounded local continuation or reject
artifact corruption                         quarantine/block/repair evidence
reference cache corruption                  invalidate/refetch exact snapshot
worker/process crash                         recover from durable stage/checkpoint
GPU/node failure                            fail attempt, reschedule, restore
kernel qualification revoked                fallback provider
checkpoint commit incomplete                ignore uncommitted staging, resume prior
```

### 44.2 Retry law

Retry is explicit and operation-aware. Generic infrastructure does not assume a
transient-looking error is safe to replay. Mutating retries require idempotency,
fencing, or a caller-proven replay-safe transaction.

---

## 45. Backpressure and boundedness

The system has no intentionally unbounded production queue, parser, task set,
or cache.

Bounds exist at:

- ingress request size;
- online concurrency;
- stage queue depth;
- broker payload and delivery attempts;
- parser input/allocation;
- worker processes;
- CPU threads;
- host/GPU memory reservations;
- object-store concurrency;
- checkpoint staging;
- reference/artifact caches;
- telemetry spools;
- callback queues;
- graceful shutdown duration.

Exhaustion produces backpressure, load shedding, retry scheduling, or fail-closed
behavior according to domain policy.

---

## 46. Determinism and reproducibility

A reproducible computation binds:

```text
source/code commit
resolved config digest
input ArtifactRefs
dataset/reference release digests
model/runtime bundle digests
toolchain/execution-platform digest
kernel provider/qualification
random seeds/RNG state
parallel topology when relevant
scientific tool versions
```

Checkpoint and release manifests carry enough identity to reconstruct these
relationships. Reproducibility does not mean hardware kernels are bit-identical
across all devices; the qualification layer records the allowed numerical
contract.

---

## 47. Qualification model

Qualification is evidence-producing and layer-specific.

### Go

- formatting, vet, race tests;
- package conformance suites;
- durable coordination/fencing tests;
- lifecycle/drain tests;
- provider integration tests in connected CI;
- architecture/dependency admission checks.

### Rust

- cargo test/clippy;
- fuzzing for untrusted parsers/protocol frames;
- Miri/sanitizer for approved unsafe boundaries;
- bounded-resource and cancellation tests;
- artifact/IPC/checkpoint integrity;
- gateway latency/load-shed/cancellation/shutdown budgets;
- node budget and stale-fence qualification.

### Python

- unit/numerical tests;
- distributed and checkpoint resume;
- configuration determinism;
- scientific pipeline/cache provenance;
- training/serving parity;
- evaluation determinism.

### TileLang

- reference parity;
- gradient parity;
- shape/layout/device signature qualification;
- compile/fake-tensor/custom-op behavior;
- performance regression budgets;
- fallback and revocation.

### Cross-language

Golden vectors cover:

```text
fault/error codes
identifiers
content digests
resource versions
ArtifactRef
request metadata
execution/admission tickets
route/revocation snapshots
worker commands/status
buffer descriptors
event envelopes
checkpoint manifests
```

---

## 48. CI and release lanes

Public CI entrypoints remain intentionally few:

```text
presubmit
GPU
nightly
security
release
```

Heavy GPU/multinode/scale qualification may run on Buildkite or dedicated
internal infrastructure, but the work remains expressed as Bazel targets.

Release promotion is impossible without required qualification artifacts.

---

## 49. End-to-end system flows

### 49.1 External source to training dataset

```text
PDB/UniProt/RNAcentral/object source
  -> Go source snapshot
  -> Rust fetch/parse/stage
  -> raw artifacts
  -> Python canonicalize/curate/quality
  -> model-ready deterministic shards
  -> Go dataset publication
  -> dataset/version/lineage evidence
```

### 49.2 Full structure prediction

```text
client submission
  -> Go durable run
  -> MSA/reference stages
  -> template stages
  -> ligand/feature stages
  -> PreprocessedInputBundle
  -> GPU admission
  -> Python model/diffusion/confidence
  -> ranking
  -> output artifacts
  -> terminal run/evaluation metadata
```

### 49.3 Online inference

```text
Go publishes route + grant
  -> Rust gateway validates/admit/routes
  -> Rust host reserves/supervises
  -> Python tensor batch/model execution
  -> qualified TileLang
  -> Rust streams/accounts/releases
```

### 49.4 Training to production release

```text
resolved config
  -> Go admission/scheduling
  -> JobSet/Kueue placement
  -> Rust node staging
  -> Python distributed trainer
  -> atomic checkpoints
  -> independent evaluation/safety
  -> model/runtime bundles
  -> evidence graph
  -> registry promotion
  -> route snapshot publication
```

---

## 50. Repository-wide invariants

The following are architectural invariants and should eventually all be
machine-enforced:

1. Go is not a synchronous dependency after online admission.
2. Python is the final authority over tensor/scientific semantics.
3. Rust cannot commit with a stale fencing token.
4. No mutable cloud path is an artifact identity.
5. No production queue or parser is unbounded.
6. Every long-running background task has an owner and shutdown path.
7. Every durable mutation that emits an event uses transactional publication.
8. Every event projection is idempotent and cursor-monotonic.
9. Reference data is versioned by immutable release digest.
10. A GPU is not reserved while long CPU preprocessing remains outstanding.
11. Runtime signed authority is locally verifiable and short-lived.
12. Revocation freshness gates new runtime admission.
13. Large tensors/batches do not travel inside control Protobuf messages.
14. Checkpoints are restorable only after atomic manifest commit.
15. Training and evaluation are separate authorities.
16. TileLang kernels cannot self-promote at runtime.
17. Resolved configuration is canonical and digest-addressed.
18. Production code does not depend on scaffold-only components.
19. Bazel is the release/build graph; Nix is toolchain authority.
20. Release promotion is based on evidence, not script exit status alone.

---

## 51. Implementation and qualification status

As of this consolidation:

- the Go foundation and durable coordination path are substantive and locally
  qualified where provider-independent;
- `control/` contains implemented source for runtime authority, routing,
  artifacts, reference releases, release-evidence validation, and unified
  orchestration seams;
- the uploaded Rust foundation has been adopted as the starting code and
  deepened with runtime/node primitives and gateway/host cores;
- deterministic Python config resolution and preprocessing contracts/DAG/cache
  provenance are implemented;
- cross-language fixtures/contracts exist for the core authority and identity
  surfaces;
- maturity, dependency-budget, Go-library admission, root-module, and build
  ownership checks are active;
- connected provider, Rust toolchain, Bazel/Nix, real cloud, performance, and
  release qualification remain explicit promotion gates where the packaging
  environment could not execute them.

See `docs/architecture/optimization-18-implementation.md`, `VALIDATION.md`, and
`QUALIFICATION.md` for the exact implementation/evidence distinction.

---

## 52. Detailed documentation map

This reference is complemented by:

| Topic | Document |
|---|---|
| architecture summary | `architecture/system-overview.md` |
| language authority | `architecture/language-boundaries.md` |
| dependency rules | `architecture/dependency-rules.md` |
| Go foundation | `architecture/go-foundation.md` |
| control plane | `architecture/control-plane.md` |
| runtime tickets/stages | `architecture/runtime-authority-and-stage-execution.md` |
| runtime data plane | `architecture/runtime-data-plane.md` |
| service boundaries | `architecture/service-boundaries.md` |
| data ingestion | `architecture/data-ingestion.md` |
| dataset publication | `architecture/dataset-publication.md` |
| preprocessing | `architecture/preprocessing.md` |
| MSA/template search | `architecture/msa-and-template-search.md` |
| reference releases/evidence | `architecture/reference-data-and-release-evidence.md` |
| serving | `architecture/serving.md` |
| training | `architecture/training.md` |
| distributed training | `architecture/distributed-training.md` |
| checkpointing | `architecture/checkpointing.md` |
| evaluation | `architecture/evaluation.md` |
| artifacts | `architecture/artifact-lifecycle.md` |
| release evidence | `architecture/release-evidence.md` |
| build/toolchains | `architecture/build-and-toolchains.md` |
| eighteen optimizations | `architecture/optimization-18-implementation.md` |
| accepted decisions | `design/decision-register.md` |
| traceability | `architecture/system-design-traceability.md` |

---

## 53. Change policy

Changes to the system design follow one of three paths:

1. **Implementation refinement within an accepted boundary** — update the local
   architecture page, tests, and readiness evidence.
2. **New reusable mechanism or dependency direction** — update machine-readable
   admission/dependency policy and document the rationale.
3. **Change of authority, durability, security, protocol, or deployment law** —
   write a new ADR that supersedes the prior decision rather than silently
   editing history.

The system should become more concrete through implementation and evidence, not
more complex through speculative abstractions.


---

## Final foundation hardening tranche

### Affected-test selection

Presubmit uses `ci/common/affected.py` to seed changed owning packages and asks
Bazel's post-loading graph for their complete reverse dependencies. It writes
separate configured-analysis and test target files; it does not parse BUILD
syntax. CI, Starlark, toolchain, lock, protocol, architecture, component,
maturity, deletion, rename, and unknown changes expand to `//...`. Merge-group,
protected-main, and daily CPU-nightly events always run `//...`. Git or Bazel
query failure is a red verdict, never an empty affected set.

### Artifact garbage collection

Artifact GC is explicitly two-phase. Go control policy determines eligibility from durable reachability, release/audit references, active leases, administrative pins, legal/audit retention holds, and retention windows. Rust receives an immutable GC plan and conditionally deletes bytes only when digest and provider generation/resource version still match the plan. Object-store prefixes never define reachability.

### Rust promotion gates

Rust promotion requires the pinned Rust 1.97.1 toolchain, a committed Cargo-generated `Cargo.lock`, `cargo metadata --locked`, rustfmt, workspace tests, Clippy, docs, supply-chain policy, compatibility matrices, fault-injection qualification, performance budgets, cross-language compatibility, and golden vertical slices. Missing connected tooling is a promotion failure, not implicit success.

### Node diagnostics and resource accounting

Node diagnostics are bounded and redacted. They expose node/runtime identity, active ticket IDs, recent process exits, cache/spool sizes, and the hierarchical resource-budget tree. The tree reports limit, reserved estimate, waiters, rejections and corruption state for node/service/worker/request accounts. Credentials, raw model weights, raw customer inputs and private keys are forbidden from diagnostics.

### Canonical workload envelope

Ingestion, preprocessing, evaluation, batch inference, checkpoint movement, artifact movement and dataset-building stages share one `WorkloadEnvelope`. Go owns durable workload lifecycle, Rust executes bounded ticketed stages, and Python supplies scientific/numerical engines. New workload types must extend this envelope instead of inventing incompatible job lifecycle semantics.

### Architecture documentation as executable policy

`architecture/enforced_decisions.toml` maps architectural statements to repository checkers. `architecture/component_ownership.toml` maps critical components to owners, language authority, SLOs, runbooks and security-review requirements. A design rule without an enforcement/evidence path is treated as documentation debt.
