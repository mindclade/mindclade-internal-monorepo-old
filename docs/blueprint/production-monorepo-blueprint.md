# Mindclade Production Monorepo Blueprint

**Status:** Final optimized target architecture<br>
**Date:** 2026-08-13<br>
**Build graph:** Bazel 9.1.1 with Bzlmod<br>
**Toolchains:** Nix flake pinned by `flake.lock`<br>
**Primary numerical runtime:** Python + PyTorch<br>
**Accelerator kernel DSL:** TileLang behind qualification gates<br>
**Control-plane language:** Go<br>
**Runtime and node data-plane language:** Rust

## 1. Executive decision

This repository is one trunk and one hermetic build graph. It owns product surfaces, public contracts, data ingestion and curation, reference-data preparation, model definitions, training, evaluation, online and batch inference, runtime services, infrastructure, and release evidence.

The optimized language law is:

```text
Go       = fleet control plane and durable policy
Rust     = online/runtime data plane and node execution
Python   = scientific, model, training, inference, and evaluation numerics
TileLang = qualified accelerator kernels
TypeScript = browser applications and public web SDKs
```

The uploaded Go foundation is adopted as the authoritative `libs/go/` implementation. Archive metadata (`__MACOSX`, `.DS_Store`) is discarded. Its Layers 0–4 remain intact, and only the generic transactional-outbox mechanism is added. Domain policy remains outside `libs/go`.

## 2. Non-negotiable architecture laws

1. **Bazel owns build, test, code generation, packaging, OCI images, qualification, release bundles, SBOMs, and provenance.**
2. **Nix owns pinned tools, compilers, interpreters, SDKs, developer shells, toolchain bundles, and remote-execution worker bases.**
3. **Bzlmod only.** No `WORKSPACE.bazel`; no nested dependency managers except independently published SDKs.
4. **Services are deployable composition roots.** Reusable domain or scientific logic never originates under `services/`.
5. **The Go control plane is never a per-request online inference dependency.** It publishes immutable route snapshots and signed bounded grants/tickets.
6. **Rust owns request and node-execution envelopes; Python owns tensor and scientific contents.**
7. **Rust performs coarse admission and compatibility grouping; Python performs final tensor-aware continuous batching.**
8. **Long preprocessing is durable asynchronous work.** MSA generation, template search, reference indexing, ligand preparation, and feature construction never hold an online GPU slot.
9. **Canonical wire contracts live under `protocols/`.** Go, Rust, Python, and TypeScript implementations are generated or checked against shared goldens.
10. **Artifacts are immutable and content-addressed.** Go owns catalog, authorization, retention policy, and lineage; Rust owns byte transfer, range reads, verification, and local cache.
11. **Unit tests are colocated.** Top-level tests cross package, process, device, language, or deployment boundaries.
12. **Research may depend on production packages; production packages may not depend on research.**
13. **No `common`, `shared`, `helpers`, or `utils` dumping grounds.** Every package has an explicit contract and owner.
14. **Every production queue, parser, buffer pool, retry loop, spool, and shutdown path is bounded.**
15. **TileLang kernels are fail-closed.** Unknown or unqualified signatures fall back to PyTorch/reference providers.

## 3. Dependency law

```text
protocols/generated ───────────────┐
                                   ├──→ sdk ───→ apps
libs/{go,python,rust,ts} ──────────┤
                                   ├──→ control ───→ services/control_plane
                                   ├──→ data ───────→ services/workers/*
                                   ├──→ preprocessing ─→ services/workers/preprocessing
                                   ├──→ kernels ────→ models
                                   ├──→ models ─────→ training / serving / evaluation
                                   ├──→ training ───→ services/workers/training
                                   ├──→ serving ────→ runtime and model-worker services
                                   └──→ evaluation ─→ services/workers/evaluation

services → may import domain packages and libraries
services → may never be imported by apps, SDKs, models, training, data, or libraries
apps     → consume SDKs and generated contracts, never services directly
```

## 4. Service ownership

| Deployable | Language | Authoritative responsibility |
|---|---|---|
| `control_plane/api` | Go | Public/admin control APIs, tenancy, run submission, registry and metadata queries |
| `control_plane/controller` | Go | Durable workflow reconciliation, leases, attempts, cancellation, publication |
| `control_plane/scheduler` | Go | Quotas, fair sharing, global capacity, placement, Kueue/JobSet policy |
| `control_plane/ingestion_controller` | Go | Source schedules, cursors, snapshots, ingestion DAG state, publication |
| `control_plane/event_dispatcher` | Go | Transactional outbox publication and durable event delivery |
| `control_plane/webhook_dispatcher` | Go | Signed webhook delivery, retries, receipts, dead-letter handling |
| `runtime_gateway` | Rust | Local ticket validation, route lookup, admission, SSE/streaming, deadlines, load shedding |
| `runtime_host` | Rust | Python process supervision, local budgets, IPC, drain, cancellation, model slots |
| `artifact_proxy` | Rust | Tenant-scoped CAS byte plane, range reads, verification, signed downloads, cache |
| `node_agent` | Rust | Reference cache, tool execution, checkpoint/data transfer, resource monitoring, diagnostics |
| `workers/ingestion` | Rust | Resumable fetch, decompression, bounded parsing, record framing, raw-artifact commit |
| `workers/curation` | Python | Scientific normalization, deduplication, safety/quality checks, dataset construction |
| `workers/preprocessing` | Python | MSA/template/ligand policy, feature construction, scientific provenance |
| `workers/reference_builder` | Python + Rust node agent | Build immutable search databases and indexes |
| `workers/model_worker` | Python/PyTorch | Final tensor batching, model loading, execution, sampling, caches |
| `workers/batch_inference` | Python/PyTorch | Durable batch inference and result construction |
| `workers/training` | Python/PyTorch | Trainer, objectives, topology plans, optimizer, numerical state |
| `workers/evaluation` | Python/PyTorch | Independent evaluation suites and release evidence |
| `workers/rollout` | Python/PyTorch | Actor inference, trajectories, policy synchronization |
| `workers/simulation` | Python | Scientific simulation environments and scoring |

Every service contains `README.md` and `PRODUCTION_READINESS.md`, declares hard resource limits, health/readiness/drain behavior, determinism, dependency boundaries, evidence targets, and explicit limitations.

## 5. Core execution contracts

The runtime contract family under `protocols/proto/mindclade/runtime/v1/` includes:

```text
ExecutionTicket
ExecutionBudget
ArtifactGrant
RouteSnapshot
WorkerCommand
WorkerStatus
BufferDescriptor
```

Go signs tickets from durable policy. Rust verifies them locally without a policy-database callback. Fencing tokens prevent stale or duplicated workers from committing statuses or artifacts after a replacement acquires a newer lease.

Control-plane outage behavior is deterministic:

```text
already-admitted work                  continues
valid unexpired execution ticket      continues within its budget
valid online grant + fresh snapshot   may admit bounded new work
new work without a valid grant        rejected
expired route or revocation snapshot  drain existing work; reject new work
full local usage/telemetry spool       reject new work; never grow unbounded
```

## 6. Comprehensive pipeline map

### 6.1 Source ingestion and dataset publication

```text
External source
  → Go source adapter discovers immutable upstream snapshot/cursor
  → Go ingestion controller creates durable stage DAG and execution tickets
  → Rust ingestion worker fetches, resumes, decompresses, bounds, hashes, frames
  → Rust artifact proxy commits immutable raw artifacts
  → Python curation worker parses scientific meaning and canonicalizes records
  → Python quality gates deduplicate, screen, validate licenses and contamination
  → Python dataset builder creates deterministic shards and manifests
  → Go registry publishes immutable dataset version and lineage
  → event outbox publishes DatasetPublished
```

Raw, canonical, curated, and model-ready outputs are distinct immutable artifact classes. Re-running curation never requires re-downloading a source snapshot when the raw artifact digest is present.

### 6.2 NovaFold and biomolecular preprocessing

```text
Input canonicalization
  → entity deduplication and cache lookup
  → protein/RNA MSA search on CPU/high-memory pools
  → profile construction
  → template search with immutable database snapshot and release-date cutoff
  → paired-MSA construction
  → CCD/ligand normalization and conformer preparation
  → model-specific featurization
  → PreprocessedInputBundle atomic commit
  → GPU inference fan-out across seeds/samples
  → confidence, ranking, artifact publication
```

Go owns the durable DAG, retries, quotas, resource class, database snapshot IDs, and cancellation. Rust owns reference-database caching, external-process supervision, byte transfer, disk/RAM limits, and cancellation enforcement. Python owns search policy, MSA filtering/pairing, template selection, ligand semantics, feature construction, and provenance.

### 6.3 Training

```text
Resolved immutable config
  → Go run authority validates quota and release policy
  → scheduler admits JobSet/Kueue workload
  → Rust node agent stages model/data/reference artifacts
  → Python engine builds model, task, optimizer, topology, and state registry
  → native/TorchTitan/Fabric adapter executes one authoritative trainer lifecycle
  → Python DCP orchestration + Rust checkpoint transfer commit checkpoint manifests
  → independent evaluation and safety suites
  → Go registry promotes or rejects release candidate
```

PyTorch Distributed Checkpoint is the semantic checkpoint mechanism; Rust accelerates transfer and verification. Checkpoints register arbitrary stateful components, RNG, loader position, topology fingerprint, optimizer state, configuration digest, and provenance.

### 6.4 Online inference

```text
Go control plane publishes signed route snapshot and bounded online grant
  → Rust gateway validates locally
  → Rust gateway performs local admission and coarse compatibility grouping
  → Rust host supervises Python model process and reserves node resources
  → Python worker creates final tensor-aware batch
  → PyTorch model uses qualified TileLang kernels or reference fallback
  → Rust multiplexes and streams results
  → usage and audit reconcile asynchronously
```

### 6.5 Batch inference

```text
Go durable job state
  → Kueue/Job admission
  → Rust node staging and artifact transfer
  → Python batch worker groups compatible items and runs model numerics
  → Rust artifact commit
  → Go metadata/registry update and event publication
```

### 6.6 Evaluation and release

```text
checkpoint/model/runtime/toolchain bundle
  → numerical parity
  → capability and robustness suites
  → biological-risk and safety gates
  → performance and scale qualification
  → SBOM/provenance/attestation bundle
  → two-person promotion decision
  → immutable release record and rollback target
```

## 7. Rust runtime rules

Production Rust uses one Tokio runtime per process, Tonic/Prost for generated control RPC, Tower for bounded middleware, Bytes for audited buffers, and Tracing/OpenTelemetry through adapters. All spawned tasks have owners; blocking work uses bounded blocking pools; all waits accept cancellation and deadlines.

A hierarchical budget tracks node → service → worker → request → operation plus shared caches. It accounts for resident and pinned memory, shared memory, buffer pools, disk, file descriptors, object-store concurrency, queues, processes, CPU threads, GPU estimates, checkpoint staging, and telemetry spool bytes.

Control IPC uses bounded Protobuf messages over gRPC or Unix sockets. Bulk data uses object references, file descriptors, local files, or shared-buffer descriptors. Large tensors and dataset batches are never embedded directly in Protobuf.

`python_bridge` remains a leaf adapter for bounded parsers, tokenizers, manifest validation, digests, and buffer descriptors. Rust does not embed the Python interpreter in the runtime host.

## 8. Go foundation policy

`libs/go` is the supplied tested implementation, with its documented dependency layers preserved. The control-plane domain packages live under `control/`; service bootstrap and provider wiring live under `services/control_plane/`.

Transport use is deliberate:

```text
grpcx    internal Go↔Go and Go↔Rust control RPC
httpx    public REST, health, webhooks, redirects
connectx optional browser-compatible protobuf RPC for selected surfaces
```

Go blob/cache/lease abstractions serve control-plane metadata and coordination. Rust is the authoritative high-throughput object and artifact byte plane.

## 9. Build, image, and release ownership

Nix produces pinned toolchain closures and remote-worker base images. Bazel consumes those bases by digest and produces every application/service/worker image, test image, model runtime bundle, release bundle, SBOM, and provenance record. Production Dockerfiles are prohibited.

Bazel targets carry tags for CPU/GPU architecture, device count, node count, hermetic/connected status, presubmit/nightly/release class, and required secrets or datasets. CI systems select Bazel targets; they do not duplicate build logic.

## 10. Materialization policy

This is the complete target-state blueprint, not an instruction to create empty placeholder packages. A path becomes live only when it has an owner, a stable contract, implementation code, a Bazel target, meaningful tests, and documentation. Packages that have not earned those properties remain represented in `docs/roadmap/` rather than as empty directories.

The supplied `libs/go` tree is already implemented and is adopted immediately. The remaining domains should be materialized vertically by end-to-end capability: ingestion → curated dataset → training → evaluation → release, and preprocessing → inference → artifact publication. This preserves production boundaries without front-loading unused organizational surface.

## 11. Materialized repository contract

The exhaustive, machine-checked path inventory is
`docs/blueprint/production-monorepo-paths.txt`. It deliberately excludes live cloud roots,
Argo CD configuration, Kubernetes environment overlays, generated deployment output, and
cluster credentials. Reusable Terraform modules and environment-neutral Kubernetes package
templates remain source artifacts in this repository.

## 12. Production acceptance gates

### Repository and build

- `bazel test //...` succeeds in the pinned Nix environment.
- Local and remote execution-platform manifests have identical digests.
- Dependency-layer and visibility checks pass.
- No undeclared host tools or network package installation occur in Bazel actions.
- Generated Protobuf, OpenAPI, AsyncAPI, JSON Schema, and SDK outputs are drift-free.

### Go

- `go test -race ./libs/go/... ./control/... ./services/control_plane/...` passes with real pinned dependencies.
- PostgreSQL, Redis, GCS, Kubernetes, gRPC, Connect, and HTTP provider suites run in connected CI.
- Transactional outbox crash recovery and stale-lease fencing are fault-injected.

### Rust

- No unbounded queue or parser allocation.
- No detached production task.
- No blocking object-store operation on async executor threads.
- No artifact accepted without digest verification.
- No stale fencing token can commit.
- Fuzz, Miri, sanitizer, Loom, and failure-injection targets pass where applicable.
- Runtime p50/p95/p99, cancellation, drain, restart, RSS, FD, copy-count, and cache-hit budgets pass.

### Python and numerics

- Eager/reference parity gates every optimized path.
- Training resume is deterministic within declared tolerance.
- Training/serving parity passes for every promoted model bundle.
- Checkpoint topology changes and partial loads are qualified.
- Hidden evaluation sets remain isolated and auditable.

### TileLang

- Every promoted kernel has a qualified signature tuple: operation, dtype, shape family, layout, architecture, compiler version.
- Numerical, gradient, aliasing, noncontiguous, ragged, NaN/Inf, compile, and fallback tests pass.
- Unknown signatures fail closed to PyTorch/reference.
- Schedule promotion, revocation, and last-known-good rollback are evidence-backed.

### Services and security

- All service readiness, liveness, drain, cancellation, and maximum-shutdown tests pass.
- Ticket expiry, route-snapshot expiry, revocation, tenant isolation, artifact grants, weight access, and break-glass paths are tested.
- SBOMs, signatures, attestations, and release evidence are attached before promotion.

## 13. Decomposition rule

The control plane begins as a modular Go system with separately deployable commands sharing one domain model and transactional database. A module becomes an independently owned service only after production evidence demonstrates at least one of:

- independently scaling load;
- materially different availability or latency objectives;
- security or data-isolation requirements;
- separate operational ownership;
- independent release cadence;
- proven database contention or failure-domain pressure.

This keeps the live repository startup-practical while retaining clean boundaries for later OpenAI/DeepMind-scale decomposition.
