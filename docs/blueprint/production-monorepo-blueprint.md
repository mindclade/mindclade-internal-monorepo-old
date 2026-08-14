# Mindclade Production Monorepo Blueprint

**Status:** Final optimized target architecture  
**Date:** 2026-08-13  
**Build graph:** Bazel 9.2.0 with Bzlmod  
**Toolchains:** Nix flake pinned by `flake.lock`  
**Primary numerical runtime:** Python + PyTorch  
**Accelerator kernel DSL:** TileLang behind qualification gates  
**Control-plane language:** Go  
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

## 11. Complete optimized repository tree

**Target-state explicit paths:** 4,475  
**Tree lines:** 5,300

```text
/
├── .buildkite/
│   ├── hooks/
│   │   ├── environment
│   │   ├── post-command
│   │   └── pre-command
│   ├── pipelines/
│   │   ├── gpu.yml
│   │   ├── nightly.yml
│   │   ├── presubmit.yml
│   │   ├── release.yml
│   │   └── security.yml
│   ├── scripts/
│   │   ├── bazel_test.sh
│   │   ├── bootstrap.sh
│   │   ├── gpu_test.sh
│   │   ├── multinode_test.sh
│   │   ├── qualification.sh
│   │   ├── release.sh
│   │   ├── security_scan.sh
│   │   └── upload_evidence.sh
│   ├── steps/
│   │   ├── common.yml
│   │   ├── cpu.yml
│   │   ├── gpu.yml
│   │   ├── multinode.yml
│   │   └── release.yml
│   ├── pipeline.yml
│   └── README.md
├── .github/
│   ├── ISSUE_TEMPLATE/
│   │   ├── bug_report.yml
│   │   ├── config.yml
│   │   ├── feature_request.yml
│   │   ├── numerical_regression.yml
│   │   ├── performance_regression.yml
│   │   ├── research_proposal.yml
│   │   └── safety_regression.yml
│   ├── workflows/
│   │   ├── gpu.yml
│   │   ├── nightly.yml
│   │   ├── presubmit.yml
│   │   ├── release.yml
│   │   └── security.yml
│   ├── BUILD.bazel
│   ├── CODEOWNERS
│   ├── dependabot.yml
│   ├── pull_request_template.md
│   └── README.md
├── apps/
│   ├── admin/
│   │   ├── src/
│   │   │   ├── app/
│   │   │   │   ├── audit/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── break-glass/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── evaluations/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── model-weights/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── organizations/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── quotas/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── releases/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── service-accounts/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── users/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── globals.css
│   │   │   │   ├── layout.tsx
│   │   │   │   └── page.tsx
│   │   │   ├── components/
│   │   │   │   ├── ApprovalGate.tsx
│   │   │   │   ├── AppShell.tsx
│   │   │   │   ├── AuditTable.tsx
│   │   │   │   ├── BreakGlassApproval.tsx
│   │   │   │   ├── ErrorBoundary.tsx
│   │   │   │   ├── EvaluationApproval.tsx
│   │   │   │   ├── LoadingState.tsx
│   │   │   │   ├── QuotaEditor.tsx
│   │   │   │   ├── ReleasePromotion.tsx
│   │   │   │   ├── StatusBadge.tsx
│   │   │   │   └── WeightAccessReview.tsx
│   │   │   └── lib/
│   │   │       ├── api.ts
│   │   │       ├── auth.ts
│   │   │       ├── events.ts
│   │   │       ├── format.ts
│   │   │       └── types.ts
│   │   ├── BUILD.bazel
│   │   ├── eslint.config.mjs
│   │   ├── next.config.ts
│   │   ├── package.json
│   │   ├── PRODUCTION_READINESS.md
│   │   ├── README.md
│   │   └── tsconfig.json
│   ├── console/
│   │   ├── src/
│   │   │   ├── app/
│   │   │   │   ├── artifacts/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── checkpoints/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── clusters/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── datasets/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── evaluations/
│   │   │   │   │   ├── [evaluationId]/
│   │   │   │   │   │   └── page.tsx
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── experiments/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── kernels/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── models/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── preprocessing/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── rollouts/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── runs/
│   │   │   │   │   ├── [runId]/
│   │   │   │   │   │   └── page.tsx
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── safety/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── serving/
│   │   │   │   │   └── page.tsx
│   │   │   │   ├── globals.css
│   │   │   │   ├── layout.tsx
│   │   │   │   └── page.tsx
│   │   │   ├── components/
│   │   │   │   ├── AppShell.tsx
│   │   │   │   ├── ArtifactTable.tsx
│   │   │   │   ├── CheckpointTable.tsx
│   │   │   │   ├── ErrorBoundary.tsx
│   │   │   │   ├── EvaluationSummary.tsx
│   │   │   │   ├── KernelQualification.tsx
│   │   │   │   ├── LoadingState.tsx
│   │   │   │   ├── MetricChart.tsx
│   │   │   │   ├── ModelTopology.tsx
│   │   │   │   ├── MolecularResult.tsx
│   │   │   │   ├── PreprocessingTimeline.tsx
│   │   │   │   ├── ResourceUsage.tsx
│   │   │   │   ├── RolloutFleet.tsx
│   │   │   │   ├── RunStatus.tsx
│   │   │   │   ├── SafetyGate.tsx
│   │   │   │   ├── StatusBadge.tsx
│   │   │   │   └── TrainingTimeline.tsx
│   │   │   └── lib/
│   │   │       ├── api.ts
│   │   │       ├── auth.ts
│   │   │       ├── events.ts
│   │   │       ├── format.ts
│   │   │       └── types.ts
│   │   ├── BUILD.bazel
│   │   ├── eslint.config.mjs
│   │   ├── next.config.ts
│   │   ├── package.json
│   │   ├── PRODUCTION_READINESS.md
│   │   ├── README.md
│   │   └── tsconfig.json
│   ├── BUILD.bazel
│   └── README.md
├── ci/
│   ├── common/
│   │   ├── affected.py
│   │   ├── BUILD.bazel
│   │   ├── environment.py
│   │   ├── evidence.py
│   │   ├── matrix.py
│   │   └── reporting.py
│   ├── gpu/
│   │   ├── BUILD.bazel
│   │   ├── pipeline.py
│   │   ├── README.md
│   │   └── targets.yaml
│   ├── nightly/
│   │   ├── BUILD.bazel
│   │   ├── pipeline.py
│   │   ├── README.md
│   │   └── targets.yaml
│   ├── presubmit/
│   │   ├── BUILD.bazel
│   │   ├── pipeline.py
│   │   ├── README.md
│   │   └── targets.yaml
│   ├── release/
│   │   ├── BUILD.bazel
│   │   ├── pipeline.py
│   │   ├── README.md
│   │   └── targets.yaml
│   ├── security/
│   │   ├── BUILD.bazel
│   │   ├── pipeline.py
│   │   ├── README.md
│   │   └── targets.yaml
│   ├── BUILD.bazel
│   └── README.md
├── configs/
│   ├── base/
│   │   ├── evaluation.toml
│   │   ├── inference.toml
│   │   ├── ingestion.toml
│   │   ├── preprocessing.toml
│   │   └── training.toml
│   ├── environments/
│   │   ├── development/
│   │   │   ├── runtime.toml
│   │   │   ├── security.toml
│   │   │   ├── storage.toml
│   │   │   └── telemetry.toml
│   │   ├── local/
│   │   │   ├── runtime.toml
│   │   │   ├── security.toml
│   │   │   ├── storage.toml
│   │   │   └── telemetry.toml
│   │   ├── production/
│   │   │   ├── runtime.toml
│   │   │   ├── security.toml
│   │   │   ├── storage.toml
│   │   │   └── telemetry.toml
│   │   └── staging/
│   │       ├── runtime.toml
│   │       ├── security.toml
│   │       ├── storage.toml
│   │       └── telemetry.toml
│   ├── evaluation/
│   │   ├── biology.toml
│   │   ├── nightly.toml
│   │   ├── presubmit.toml
│   │   ├── release.toml
│   │   └── safety.toml
│   ├── profiles/
│   │   ├── data/
│   │   │   ├── biomolecular.toml
│   │   │   ├── object-store-streaming.toml
│   │   │   ├── sequence-pretraining.toml
│   │   │   └── synthetic.toml
│   │   ├── hardware/
│   │   │   ├── a100-8x.toml
│   │   │   ├── b200-8x.toml
│   │   │   ├── cpu.toml
│   │   │   ├── h100-8x.toml
│   │   │   ├── h200-8x.toml
│   │   │   └── mi300x-8x.toml
│   │   ├── ingestion/
│   │   │   ├── bulk.toml
│   │   │   ├── interactive.toml
│   │   │   └── mirror.toml
│   │   ├── kernels/
│   │   │   ├── pytorch-reference.toml
│   │   │   ├── tilelang-blackwell.toml
│   │   │   ├── tilelang-default.toml
│   │   │   ├── tilelang-hopper.toml
│   │   │   └── tilelang-mi300x.toml
│   │   ├── parallelism/
│   │   │   ├── ddp.toml
│   │   │   ├── expert-parallel.toml
│   │   │   ├── fsdp2.toml
│   │   │   ├── pipeline-parallel.toml
│   │   │   ├── single-process.toml
│   │   │   ├── tensor-parallel.toml
│   │   │   └── three-dimensional.toml
│   │   ├── precision/
│   │   │   ├── bf16.toml
│   │   │   ├── fp16.toml
│   │   │   ├── fp32.toml
│   │   │   ├── fp8.toml
│   │   │   └── tf32.toml
│   │   ├── preprocessing/
│   │   │   ├── novafold-deep.toml
│   │   │   ├── novafold-default.toml
│   │   │   └── novafold-fast.toml
│   │   ├── runtime/
│   │   │   ├── gke.toml
│   │   │   ├── kubernetes.toml
│   │   │   ├── local.toml
│   │   │   ├── remote-execution.toml
│   │   │   └── slurm.toml
│   │   ├── serving/
│   │   │   ├── batch.toml
│   │   │   ├── canary.toml
│   │   │   ├── realtime.toml
│   │   │   └── rollout.toml
│   │   └── storage/
│   │       ├── gcs.toml
│   │       ├── local.toml
│   │       └── s3.toml
│   ├── recipes/
│   │   ├── biology/
│   │   │   ├── clade-1-pretrain.toml
│   │   │   ├── novafold-inference.toml
│   │   │   └── novafold-train.toml
│   │   ├── data/
│   │   │   ├── biomolecular-dataset.toml
│   │   │   ├── pdb-sync.toml
│   │   │   ├── rnacentral-sync.toml
│   │   │   └── uniprot-sync.toml
│   │   ├── diffusion/
│   │   │   ├── diffusion-finetune.toml
│   │   │   └── diffusion-pretrain.toml
│   │   ├── multimodal/
│   │   │   └── multimodal-pretrain.toml
│   │   ├── post_training/
│   │   │   ├── distillation.toml
│   │   │   ├── preference-optimization.toml
│   │   │   └── sft.toml
│   │   ├── pretraining/
│   │   │   ├── dense-debug.toml
│   │   │   ├── dense-multinode.toml
│   │   │   └── moe-multinode.toml
│   │   └── reinforcement/
│   │       ├── grpo.toml
│   │       ├── ppo.toml
│   │       ├── rl-colocated.toml
│   │       └── rl-disaggregated.toml
│   ├── release/
│   │   ├── model-bundle.toml
│   │   ├── qualification.toml
│   │   ├── rollback.toml
│   │   ├── runtime-bundle.toml
│   │   ├── signing.toml
│   │   └── training-image.toml
│   ├── schemas/
│   │   ├── dataset.schema.json
│   │   ├── evaluation.schema.json
│   │   ├── ingestion.schema.json
│   │   ├── preprocessing.schema.json
│   │   ├── release.schema.json
│   │   ├── run.schema.json
│   │   └── serving.schema.json
│   ├── BUILD.bazel
│   └── README.md
├── control/
│   ├── admission/
│   │   ├── admission_test.go
│   │   ├── budget.go
│   │   ├── BUILD.bazel
│   │   ├── doc.go
│   │   ├── entitlement.go
│   │   ├── quota.go
│   │   ├── README.md
│   │   ├── repository.go
│   │   ├── reservation.go
│   │   ├── service.go
│   │   └── validation.go
│   ├── artifacts/
│   │   ├── artifacts_test.go
│   │   ├── BUILD.bazel
│   │   ├── catalog.go
│   │   ├── doc.go
│   │   ├── grant.go
│   │   ├── README.md
│   │   ├── repository.go
│   │   ├── retention.go
│   │   ├── service.go
│   │   └── validation.go
│   ├── audit/
│   │   ├── audit_test.go
│   │   ├── BUILD.bazel
│   │   ├── doc.go
│   │   ├── model.go
│   │   ├── README.md
│   │   ├── repository.go
│   │   └── service.go
│   ├── evaluations/
│   │   ├── BUILD.bazel
│   │   ├── doc.go
│   │   ├── evaluations_test.go
│   │   ├── model.go
│   │   ├── README.md
│   │   ├── repository.go
│   │   ├── service.go
│   │   └── state.go
│   ├── events/
│   │   ├── BUILD.bazel
│   │   ├── dispatcher.go
│   │   ├── doc.go
│   │   ├── envelope.go
│   │   ├── events_test.go
│   │   ├── publisher.go
│   │   ├── README.md
│   │   ├── service.go
│   │   └── subscription.go
│   ├── ingestion/
│   │   ├── adapters/
│   │   │   ├── kubernetes/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── job.go
│   │   │   │   └── resources.go
│   │   │   ├── BUILD.bazel
│   │   │   └── README.md
│   │   ├── BUILD.bazel
│   │   ├── cursor.go
│   │   ├── doc.go
│   │   ├── pipeline.go
│   │   ├── pipeline_test.go
│   │   ├── README.md
│   │   ├── repository.go
│   │   ├── service.go
│   │   ├── snapshot.go
│   │   ├── source.go
│   │   ├── stage.go
│   │   ├── state.go
│   │   ├── state_test.go
│   │   └── validation.go
│   ├── lineage/
│   │   ├── BUILD.bazel
│   │   ├── doc.go
│   │   ├── edge.go
│   │   ├── graph.go
│   │   ├── lineage_test.go
│   │   ├── README.md
│   │   ├── repository.go
│   │   └── service.go
│   ├── metadata/
│   │   ├── BUILD.bazel
│   │   ├── doc.go
│   │   ├── metadata_test.go
│   │   ├── model.go
│   │   ├── query.go
│   │   ├── README.md
│   │   ├── repository.go
│   │   └── service.go
│   ├── orchestration/
│   │   ├── adapters/
│   │   │   ├── kubernetes/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── jobset.go
│   │   │   │   ├── kueue.go
│   │   │   │   ├── launcher.go
│   │   │   │   ├── README.md
│   │   │   │   └── resources.go
│   │   │   ├── local/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── launcher.go
│   │   │   │   └── README.md
│   │   │   └── slurm/
│   │   │       ├── BUILD.bazel
│   │   │       ├── launcher.go
│   │   │       └── README.md
│   │   ├── attempt.go
│   │   ├── BUILD.bazel
│   │   ├── cancellation.go
│   │   ├── compiler.go
│   │   ├── dependency.go
│   │   ├── doc.go
│   │   ├── executor.go
│   │   ├── lease.go
│   │   ├── README.md
│   │   ├── repository.go
│   │   ├── service.go
│   │   ├── stage.go
│   │   ├── state_machine.go
│   │   ├── state_machine_test.go
│   │   ├── validation.go
│   │   ├── workflow.go
│   │   └── workflow_test.go
│   ├── registry/
│   │   ├── checkpoints/
│   │   │   ├── BUILD.bazel
│   │   │   ├── model.go
│   │   │   ├── policy.go
│   │   │   ├── service.go
│   │   │   ├── service_test.go
│   │   │   └── validation.go
│   │   ├── datasets/
│   │   │   ├── BUILD.bazel
│   │   │   ├── model.go
│   │   │   ├── policy.go
│   │   │   ├── service.go
│   │   │   ├── service_test.go
│   │   │   └── validation.go
│   │   ├── deployments/
│   │   │   ├── BUILD.bazel
│   │   │   ├── model.go
│   │   │   ├── policy.go
│   │   │   ├── service.go
│   │   │   ├── service_test.go
│   │   │   └── validation.go
│   │   ├── models/
│   │   │   ├── BUILD.bazel
│   │   │   ├── model.go
│   │   │   ├── policy.go
│   │   │   ├── service.go
│   │   │   ├── service_test.go
│   │   │   └── validation.go
│   │   ├── reference_databases/
│   │   │   ├── BUILD.bazel
│   │   │   ├── model.go
│   │   │   ├── policy.go
│   │   │   ├── service.go
│   │   │   ├── service_test.go
│   │   │   └── validation.go
│   │   ├── releases/
│   │   │   ├── BUILD.bazel
│   │   │   ├── model.go
│   │   │   ├── policy.go
│   │   │   ├── service.go
│   │   │   ├── service_test.go
│   │   │   └── validation.go
│   │   ├── BUILD.bazel
│   │   ├── doc.go
│   │   ├── README.md
│   │   ├── registry_test.go
│   │   ├── repository.go
│   │   ├── service.go
│   │   └── validation.go
│   ├── routing/
│   │   ├── BUILD.bazel
│   │   ├── deployment.go
│   │   ├── doc.go
│   │   ├── policy.go
│   │   ├── policy_test.go
│   │   ├── publisher.go
│   │   ├── README.md
│   │   ├── repository.go
│   │   ├── route.go
│   │   ├── service.go
│   │   ├── snapshot.go
│   │   ├── snapshot_test.go
│   │   └── validation.go
│   ├── runs/
│   │   ├── BUILD.bazel
│   │   ├── doc.go
│   │   ├── job.go
│   │   ├── README.md
│   │   ├── repository.go
│   │   ├── run.go
│   │   ├── runs_test.go
│   │   ├── service.go
│   │   ├── state.go
│   │   └── validation.go
│   ├── runtime_authority/
│   │   ├── admission_grant.go
│   │   ├── artifact_grant.go
│   │   ├── BUILD.bazel
│   │   ├── doc.go
│   │   ├── execution_ticket.go
│   │   ├── fencing.go
│   │   ├── golden_test.go
│   │   ├── issuer.go
│   │   ├── keyring.go
│   │   ├── README.md
│   │   ├── revocation.go
│   │   ├── route_snapshot.go
│   │   ├── signer.go
│   │   ├── ticket_test.go
│   │   └── validation.go
│   ├── scheduling/
│   │   ├── adapters/
│   │   │   ├── jobset/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── client.go
│   │   │   │   ├── jobs.go
│   │   │   │   └── topology.go
│   │   │   └── kueue/
│   │   │       ├── BUILD.bazel
│   │   │       ├── client.go
│   │   │       ├── queues.go
│   │   │       └── workloads.go
│   │   ├── admission.go
│   │   ├── BUILD.bazel
│   │   ├── capacity.go
│   │   ├── doc.go
│   │   ├── fair_share_test.go
│   │   ├── placement.go
│   │   ├── placement_test.go
│   │   ├── pool.go
│   │   ├── preemption.go
│   │   ├── priority.go
│   │   ├── README.md
│   │   ├── repository.go
│   │   ├── reservation.go
│   │   ├── service.go
│   │   └── topology.go
│   ├── tenancy/
│   │   ├── BUILD.bazel
│   │   ├── doc.go
│   │   ├── organization.go
│   │   ├── project.go
│   │   ├── README.md
│   │   ├── repository.go
│   │   ├── service.go
│   │   ├── service_account.go
│   │   ├── tenancy_test.go
│   │   └── workspace.go
│   ├── usage/
│   │   ├── aggregation.go
│   │   ├── BUILD.bazel
│   │   ├── doc.go
│   │   ├── meter.go
│   │   ├── README.md
│   │   ├── record.go
│   │   ├── repository.go
│   │   ├── service.go
│   │   └── usage_test.go
│   ├── webhooks/
│   │   ├── BUILD.bazel
│   │   ├── delivery.go
│   │   ├── doc.go
│   │   ├── endpoint.go
│   │   ├── README.md
│   │   ├── repository.go
│   │   ├── service.go
│   │   ├── signature.go
│   │   ├── subscription.go
│   │   └── webhooks_test.go
│   ├── weights/
│   │   ├── access.go
│   │   ├── BUILD.bazel
│   │   ├── doc.go
│   │   ├── grant.go
│   │   ├── policy.go
│   │   ├── README.md
│   │   ├── repository.go
│   │   ├── service.go
│   │   └── weights_test.go
│   ├── BUILD.bazel
│   └── README.md
├── data/
│   ├── connectors/
│   │   ├── gcs/
│   │   │   ├── BUILD.bazel
│   │   │   ├── client.go
│   │   │   ├── cursor.go
│   │   │   ├── doc.go
│   │   │   ├── gcs_test.go
│   │   │   ├── README.md
│   │   │   └── snapshot.go
│   │   ├── http/
│   │   │   ├── BUILD.bazel
│   │   │   ├── client.go
│   │   │   ├── cursor.go
│   │   │   ├── doc.go
│   │   │   ├── http_test.go
│   │   │   ├── README.md
│   │   │   └── snapshot.go
│   │   ├── huggingface/
│   │   │   ├── BUILD.bazel
│   │   │   ├── client.go
│   │   │   ├── cursor.go
│   │   │   ├── doc.go
│   │   │   ├── huggingface_test.go
│   │   │   ├── README.md
│   │   │   └── snapshot.go
│   │   ├── pdb/
│   │   │   ├── BUILD.bazel
│   │   │   ├── catalog.go
│   │   │   ├── cursor.go
│   │   │   ├── doc.go
│   │   │   ├── license.go
│   │   │   ├── pdb_test.go
│   │   │   ├── README.md
│   │   │   └── snapshot.go
│   │   ├── rnacentral/
│   │   │   ├── BUILD.bazel
│   │   │   ├── catalog.go
│   │   │   ├── cursor.go
│   │   │   ├── doc.go
│   │   │   ├── license.go
│   │   │   ├── README.md
│   │   │   ├── rnacentral_test.go
│   │   │   └── snapshot.go
│   │   ├── s3/
│   │   │   ├── BUILD.bazel
│   │   │   ├── client.go
│   │   │   ├── cursor.go
│   │   │   ├── doc.go
│   │   │   ├── README.md
│   │   │   ├── s3_test.go
│   │   │   └── snapshot.go
│   │   ├── uniprot/
│   │   │   ├── BUILD.bazel
│   │   │   ├── catalog.go
│   │   │   ├── cursor.go
│   │   │   ├── doc.go
│   │   │   ├── license.go
│   │   │   ├── README.md
│   │   │   ├── snapshot.go
│   │   │   └── uniprot_test.go
│   │   ├── BUILD.bazel
│   │   └── README.md
│   ├── contracts/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_contracts.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── dataset.py
│   │   ├── README.md
│   │   ├── record.py
│   │   ├── shard.py
│   │   ├── snapshot.py
│   │   ├── source.py
│   │   └── validation.py
│   ├── curation/
│   │   ├── sources/
│   │   │   ├── __init__.py
│   │   │   ├── internal.py
│   │   │   ├── pdb.py
│   │   │   ├── rnacentral.py
│   │   │   └── uniprot.py
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_deduplication.py
│   │   │   └── test_pipeline.py
│   │   ├── __init__.py
│   │   ├── augmentation.py
│   │   ├── BUILD.bazel
│   │   ├── consent.py
│   │   ├── contamination.py
│   │   ├── deduplication.py
│   │   ├── filtering.py
│   │   ├── fingerprints.py
│   │   ├── licensing.py
│   │   ├── normalization.py
│   │   ├── pipeline.py
│   │   ├── provenance.py
│   │   ├── README.md
│   │   └── safety_screening.py
│   ├── datasets/
│   │   ├── cards/
│   │   │   ├── biomolecular-complexes.md
│   │   │   ├── hidden-evaluation.md
│   │   │   ├── README.md
│   │   │   ├── rollout-trajectories.md
│   │   │   ├── sequence-pretraining.md
│   │   │   └── simulation-environments.md
│   │   ├── manifests/
│   │   │   ├── biomolecular-complexes.example.json
│   │   │   ├── README.md
│   │   │   ├── rollout-trajectories.example.json
│   │   │   └── sequence-pretraining.example.json
│   │   ├── mixtures/
│   │   │   ├── biomolecular-multimodal.toml
│   │   │   ├── debug.toml
│   │   │   └── sequence-pretraining.toml
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_catalog.py
│   │   │   └── test_resolver.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── catalog.py
│   │   ├── lineage.py
│   │   ├── mixture.py
│   │   ├── README.md
│   │   ├── registry.py
│   │   ├── resolver.py
│   │   └── versioning.py
│   ├── ingestion/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_pipeline.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── canonical.py
│   │   ├── pipeline.py
│   │   ├── publication.py
│   │   ├── raw.py
│   │   ├── README.md
│   │   ├── record.py
│   │   ├── stages.py
│   │   └── validation.py
│   ├── loaders/
│   │   ├── experience/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_experience.py
│   │   │   ├── __init__.py
│   │   │   ├── batching.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── policy_version.py
│   │   │   ├── README.md
│   │   │   ├── replay.py
│   │   │   ├── resume.py
│   │   │   ├── sampling.py
│   │   │   └── trajectory.py
│   │   ├── packing/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_packing.py
│   │   │   ├── __init__.py
│   │   │   ├── bin_packing.py
│   │   │   ├── boundaries.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── multimodal.py
│   │   │   ├── README.md
│   │   │   └── sequence.py
│   │   ├── sampling/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_sampling.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── curriculum.py
│   │   │   ├── deterministic.py
│   │   │   ├── mixture.py
│   │   │   ├── random.py
│   │   │   ├── README.md
│   │   │   ├── temperature.py
│   │   │   └── weighted.py
│   │   ├── sharding/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_sharding.py
│   │   │   ├── __init__.py
│   │   │   ├── assignment.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── rank_partition.py
│   │   │   ├── README.md
│   │   │   ├── rebalance.py
│   │   │   ├── resume.py
│   │   │   └── worker_partition.py
│   │   ├── streaming/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_streaming.py
│   │   │   ├── __init__.py
│   │   │   ├── backpressure.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── checkpoint.py
│   │   │   ├── iterator.py
│   │   │   ├── prefetch.py
│   │   │   ├── reader.py
│   │   │   ├── README.md
│   │   │   └── shuffle.py
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_loader.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── collate.py
│   │   ├── device_prefetch.py
│   │   ├── diagnostics.py
│   │   ├── loader.py
│   │   ├── pinned_memory.py
│   │   ├── README.md
│   │   └── workers.py
│   ├── quality/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_gates.py
│   │   │   └── test_integrity.py
│   │   ├── __init__.py
│   │   ├── bias.py
│   │   ├── biological_safety.py
│   │   ├── BUILD.bazel
│   │   ├── drift.py
│   │   ├── evaluation_leakage.py
│   │   ├── gates.py
│   │   ├── hidden_set_integrity.py
│   │   ├── integrity.py
│   │   ├── leakage.py
│   │   ├── license_compliance.py
│   │   ├── privacy.py
│   │   ├── README.md
│   │   ├── report.py
│   │   ├── schema.py
│   │   ├── statistics.py
│   │   └── validators.py
│   ├── reference/
│   │   ├── builders/
│   │   │   ├── __init__.py
│   │   │   ├── chemical_components.py
│   │   │   ├── msa_databases.py
│   │   │   └── template_database.py
│   │   ├── manifests/
│   │   │   ├── ccd.example.json
│   │   │   ├── pdb.example.json
│   │   │   ├── README.md
│   │   │   ├── rnacentral.example.json
│   │   │   └── uniref.example.json
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_manifest.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── builder.py
│   │   ├── catalog.py
│   │   ├── index.py
│   │   ├── manifest.py
│   │   ├── README.md
│   │   ├── snapshot.py
│   │   ├── source.py
│   │   └── validation.py
│   ├── tokenizers/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_tokenizers.py
│   │   ├── __init__.py
│   │   ├── api.py
│   │   ├── BUILD.bazel
│   │   ├── chemistry.py
│   │   ├── dna_rna.py
│   │   ├── multimodal.py
│   │   ├── protein.py
│   │   ├── README.md
│   │   ├── registry.py
│   │   ├── special_tokens.py
│   │   ├── structure.py
│   │   ├── text.py
│   │   └── vocabulary.py
│   ├── __init__.py
│   ├── api.py
│   ├── batch.py
│   ├── BUILD.bazel
│   ├── manifest.py
│   ├── py.typed
│   ├── README.md
│   └── sample.py
├── docs/
│   ├── architecture/
│   │   ├── artifact-lifecycle.md
│   │   ├── build-and-toolchains.md
│   │   ├── checkpointing.md
│   │   ├── control-plane.md
│   │   ├── data-ingestion.md
│   │   ├── dataset-publication.md
│   │   ├── dependency-rules.md
│   │   ├── distributed-training.md
│   │   ├── evaluation.md
│   │   ├── language-boundaries.md
│   │   ├── msa-and-template-search.md
│   │   ├── preprocessing.md
│   │   ├── release-evidence.md
│   │   ├── runtime-data-plane.md
│   │   ├── service-boundaries.md
│   │   ├── serving.md
│   │   ├── system-overview.md
│   │   └── training.md
│   ├── design/
│   │   ├── adr-0001-bazel-bzlmod.md
│   │   ├── adr-0002-nix-owned-toolchains.md
│   │   ├── adr-0003-polyglot-boundaries.md
│   │   ├── adr-0004-content-addressed-artifacts.md
│   │   ├── adr-0005-execution-tickets.md
│   │   ├── adr-0006-rust-runtime-data-plane.md
│   │   ├── adr-0007-python-numerical-authority.md
│   │   ├── adr-0008-qualified-tilelang-kernels.md
│   │   ├── adr-0009-go-library-layers.md
│   │   ├── adr-0010-services-are-composition-roots.md
│   │   └── rfc-template.md
│   ├── roadmap/
│   │   ├── decomposition-triggers.md
│   │   ├── README.md
│   │   └── scale-milestones.md
│   ├── runbooks/
│   │   ├── artifact-corruption.md
│   │   ├── checkpoint-failure.md
│   │   ├── control-plane-outage.md
│   │   ├── data-ingestion-stalled.md
│   │   ├── gpu-health.md
│   │   ├── node-preemption.md
│   │   ├── preprocessing-stalled.md
│   │   ├── reference-cache-corruption.md
│   │   ├── release-rollback.md
│   │   ├── runtime-gateway-degraded.md
│   │   ├── serving-latency.md
│   │   ├── ticket-key-rotation.md
│   │   └── training-stalled.md
│   ├── security/
│   │   ├── execution-ticket-security.md
│   │   ├── model-weight-access.md
│   │   ├── supply-chain.md
│   │   ├── tenant-isolation.md
│   │   └── threat-model.md
│   ├── theme/
│   │   ├── extra.css
│   │   └── main.html
│   ├── BUILD.bazel
│   ├── index.md
│   ├── mkdocs.yml
│   └── README.md
├── evaluation/
│   ├── biological_risk/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_biological_risk.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── capabilities.py
│   │   ├── gates.py
│   │   ├── README.md
│   │   ├── report.py
│   │   └── screening.py
│   ├── contracts/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_contracts.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── case.py
│   │   ├── evaluator.py
│   │   ├── metric.py
│   │   ├── README.md
│   │   ├── report.py
│   │   ├── result.py
│   │   └── suite.py
│   ├── external/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_external.py
│   │   ├── __init__.py
│   │   ├── adapter.py
│   │   ├── BUILD.bazel
│   │   ├── ingestion.py
│   │   ├── README.md
│   │   ├── submission.py
│   │   └── verification.py
│   ├── harness/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_runner.py
│   │   ├── __init__.py
│   │   ├── batching.py
│   │   ├── BUILD.bazel
│   │   ├── caching.py
│   │   ├── distributed.py
│   │   ├── evaluator.py
│   │   ├── generation.py
│   │   ├── isolation.py
│   │   ├── README.md
│   │   ├── runner.py
│   │   └── sampling.py
│   ├── metrics/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_metrics.py
│   │   ├── __init__.py
│   │   ├── aggregation.py
│   │   ├── BUILD.bazel
│   │   ├── calibration.py
│   │   ├── classification.py
│   │   ├── distribution.py
│   │   ├── generation.py
│   │   ├── ranking.py
│   │   ├── README.md
│   │   └── structure.py
│   ├── privacy/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_privacy.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── leakage.py
│   │   ├── membership.py
│   │   ├── memorization.py
│   │   ├── README.md
│   │   └── report.py
│   ├── qualification/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_qualification.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── evidence.py
│   │   ├── promotion.py
│   │   ├── README.md
│   │   ├── release_gate.py
│   │   ├── thresholds.py
│   │   └── verification.py
│   ├── regression/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_regression.py
│   │   ├── __init__.py
│   │   ├── baseline.py
│   │   ├── BUILD.bazel
│   │   ├── comparator.py
│   │   ├── gate.py
│   │   ├── README.md
│   │   └── thresholds.py
│   ├── reporting/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_reporting.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── html_report.py
│   │   ├── json_report.py
│   │   ├── markdown_report.py
│   │   ├── README.md
│   │   └── summary.py
│   ├── robustness/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_robustness.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── metrics.py
│   │   ├── perturbations.py
│   │   ├── README.md
│   │   ├── report.py
│   │   └── runner.py
│   ├── safety/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_safety.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── gates.py
│   │   ├── policy.py
│   │   ├── README.md
│   │   ├── report.py
│   │   └── runner.py
│   ├── simulation/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_simulation.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── environment.py
│   │   ├── harness.py
│   │   ├── isolation.py
│   │   ├── README.md
│   │   ├── replay.py
│   │   └── scoring.py
│   ├── suites/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_suites.py
│   │   ├── __init__.py
│   │   ├── biology.py
│   │   ├── BUILD.bazel
│   │   ├── capability.py
│   │   ├── language.py
│   │   ├── multimodal.py
│   │   ├── nightly.py
│   │   ├── README.md
│   │   ├── release.py
│   │   ├── robustness.py
│   │   ├── safety.py
│   │   └── smoke.py
│   ├── __init__.py
│   ├── api.py
│   ├── BUILD.bazel
│   ├── py.typed
│   ├── README.md
│   └── registry.py
├── infra/
│   ├── gitops/
│   │   ├── argocd/
│   │   │   ├── app-of-apps.yaml
│   │   │   ├── projects.yaml
│   │   │   └── repositories.yaml
│   │   ├── environments/
│   │   │   ├── development/
│   │   │   │   ├── applications.yaml
│   │   │   │   └── kustomization.yaml
│   │   │   ├── production/
│   │   │   │   ├── applications.yaml
│   │   │   │   └── kustomization.yaml
│   │   │   └── staging/
│   │   │       ├── applications.yaml
│   │   │       └── kustomization.yaml
│   │   ├── BUILD.bazel
│   │   └── README.md
│   ├── kubernetes/
│   │   ├── base/
│   │   │   ├── kustomization.yaml
│   │   │   ├── namespace.yaml
│   │   │   ├── network-policies.yaml
│   │   │   ├── rbac.yaml
│   │   │   └── service-accounts.yaml
│   │   ├── overlays/
│   │   │   ├── development/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── kustomization.yaml
│   │   │   │   └── patches.yaml
│   │   │   ├── production/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── kustomization.yaml
│   │   │   │   └── patches.yaml
│   │   │   └── staging/
│   │   │       ├── BUILD.bazel
│   │   │       ├── kustomization.yaml
│   │   │       └── patches.yaml
│   │   ├── platform/
│   │   │   ├── gpu/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── README.md
│   │   │   │   └── resources.yaml
│   │   │   ├── jobset/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── README.md
│   │   │   │   └── resources.yaml
│   │   │   ├── kueue/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── README.md
│   │   │   │   └── resources.yaml
│   │   │   ├── nccl/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── README.md
│   │   │   │   └── resources.yaml
│   │   │   ├── rdma/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── README.md
│   │   │   │   └── resources.yaml
│   │   │   ├── remote-execution/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── README.md
│   │   │   │   └── resources.yaml
│   │   │   └── security/
│   │   │       ├── BUILD.bazel
│   │   │       ├── kustomization.yaml
│   │   │       ├── README.md
│   │   │       └── resources.yaml
│   │   ├── services/
│   │   │   ├── artifact-proxy/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── deployment.yaml
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── network-policy.yaml
│   │   │   │   ├── pod-disruption-budget.yaml
│   │   │   │   ├── service-account.yaml
│   │   │   │   └── service.yaml
│   │   │   ├── control-plane/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── deployment.yaml
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── network-policy.yaml
│   │   │   │   ├── pod-disruption-budget.yaml
│   │   │   │   ├── service-account.yaml
│   │   │   │   └── service.yaml
│   │   │   ├── curation-workers/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── deployment.yaml
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── network-policy.yaml
│   │   │   │   ├── pod-disruption-budget.yaml
│   │   │   │   ├── service-account.yaml
│   │   │   │   └── service.yaml
│   │   │   ├── evaluation-workers/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── deployment.yaml
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── network-policy.yaml
│   │   │   │   ├── pod-disruption-budget.yaml
│   │   │   │   ├── service-account.yaml
│   │   │   │   └── service.yaml
│   │   │   ├── ingestion-workers/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── deployment.yaml
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── network-policy.yaml
│   │   │   │   ├── pod-disruption-budget.yaml
│   │   │   │   ├── service-account.yaml
│   │   │   │   └── service.yaml
│   │   │   ├── node-agent/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── deployment.yaml
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── network-policy.yaml
│   │   │   │   ├── pod-disruption-budget.yaml
│   │   │   │   ├── service-account.yaml
│   │   │   │   └── service.yaml
│   │   │   ├── preprocessing-workers/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── deployment.yaml
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── network-policy.yaml
│   │   │   │   ├── pod-disruption-budget.yaml
│   │   │   │   ├── service-account.yaml
│   │   │   │   └── service.yaml
│   │   │   ├── rollout-workers/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── deployment.yaml
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── network-policy.yaml
│   │   │   │   ├── pod-disruption-budget.yaml
│   │   │   │   ├── service-account.yaml
│   │   │   │   └── service.yaml
│   │   │   ├── runtime-gateway/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── deployment.yaml
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── network-policy.yaml
│   │   │   │   ├── pod-disruption-budget.yaml
│   │   │   │   ├── service-account.yaml
│   │   │   │   └── service.yaml
│   │   │   ├── runtime-host/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── deployment.yaml
│   │   │   │   ├── kustomization.yaml
│   │   │   │   ├── network-policy.yaml
│   │   │   │   ├── pod-disruption-budget.yaml
│   │   │   │   ├── service-account.yaml
│   │   │   │   └── service.yaml
│   │   │   └── training-workers/
│   │   │       ├── BUILD.bazel
│   │   │       ├── deployment.yaml
│   │   │       ├── kustomization.yaml
│   │   │       ├── network-policy.yaml
│   │   │       ├── pod-disruption-budget.yaml
│   │   │       ├── service-account.yaml
│   │   │       └── service.yaml
│   │   ├── tests/
│   │   │   ├── policy-test.yaml
│   │   │   └── validate.sh
│   │   ├── workloads/
│   │   │   ├── ingestion/
│   │   │   │   ├── bulk-sync-job.yaml
│   │   │   │   └── curation-job.yaml
│   │   │   ├── preprocessing/
│   │   │   │   ├── cpu-search-job.yaml
│   │   │   │   ├── feature-job.yaml
│   │   │   │   └── reference-cache.yaml
│   │   │   └── training/
│   │   │       ├── checkpoint-sidecar.yaml
│   │   │       ├── jobset-template.yaml
│   │   │       └── torchrun-template.yaml
│   │   ├── BUILD.bazel
│   │   └── README.md
│   ├── observability/
│   │   ├── alerts/
│   │   │   ├── artifact-corruption.yaml
│   │   │   ├── checkpoint-failed.yaml
│   │   │   ├── control-plane-degraded.yaml
│   │   │   ├── data-starvation.yaml
│   │   │   ├── ingestion-stalled.yaml
│   │   │   ├── kernel-regression.yaml
│   │   │   ├── preprocessing-stalled.yaml
│   │   │   ├── release-gate-failed.yaml
│   │   │   ├── serving-slo.yaml
│   │   │   └── training-stalled.yaml
│   │   ├── dashboards/
│   │   │   ├── artifacts.json
│   │   │   ├── build-and-ci.json
│   │   │   ├── control-plane.json
│   │   │   ├── data-pipeline.json
│   │   │   ├── evaluation.json
│   │   │   ├── kernels.json
│   │   │   ├── preprocessing.json
│   │   │   ├── serving.json
│   │   │   └── training.json
│   │   ├── BUILD.bazel
│   │   ├── grafana-datasources.yaml
│   │   ├── otel-collector.yaml
│   │   ├── prometheus-rules.yaml
│   │   └── README.md
│   ├── security/
│   │   ├── kyverno/
│   │   │   └── kustomization.yaml
│   │   ├── opa/
│   │   │   └── policy.rego
│   │   ├── tests/
│   │   │   ├── test_attestation.py
│   │   │   ├── test_break_glass.py
│   │   │   └── test_weight_access.py
│   │   ├── audit-retention.yaml
│   │   ├── break-glass.yaml
│   │   ├── BUILD.bazel
│   │   ├── image-policy.yaml
│   │   ├── model-weight-access.yaml
│   │   ├── network-policies.yaml
│   │   ├── node-attestation.yaml
│   │   ├── pod-security.yaml
│   │   ├── README.md
│   │   ├── secrets-rotation.yaml
│   │   ├── supply-chain-policy.yaml
│   │   └── threat-model.md
│   ├── terraform/
│   │   ├── environments/
│   │   │   ├── development/
│   │   │   │   ├── backend.tf
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   └── variables.tf
│   │   │   ├── production/
│   │   │   │   ├── backend.tf
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   └── variables.tf
│   │   │   └── staging/
│   │   │       ├── backend.tf
│   │   │       ├── BUILD.bazel
│   │   │       ├── main.tf
│   │   │       ├── outputs.tf
│   │   │       └── variables.tf
│   │   ├── modules/
│   │   │   ├── artifact_registry/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── audit_archive/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── bazel_remote_cache/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── bazel_remote_execution/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── cpu_node_pool/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── gke/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── gpu_node_pool/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── kms/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── network/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── nix_binary_cache/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── object_storage/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── observability/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── organization/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── postgres/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── pubsub/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── redis/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   ├── secret_manager/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── main.tf
│   │   │   │   ├── outputs.tf
│   │   │   │   ├── README.md
│   │   │   │   └── variables.tf
│   │   │   └── workload_identity/
│   │   │       ├── BUILD.bazel
│   │   │       ├── main.tf
│   │   │       ├── outputs.tf
│   │   │       ├── README.md
│   │   │       └── variables.tf
│   │   ├── tests/
│   │   │   └── main.tftest.hcl
│   │   ├── BUILD.bazel
│   │   ├── providers.tf
│   │   ├── README.md
│   │   └── versions.tf
│   ├── BUILD.bazel
│   └── README.md
├── kernels/
│   ├── api/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_api.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── capabilities.py
│   │   ├── custom_ops.py
│   │   ├── errors.py
│   │   ├── fake_tensor.py
│   │   ├── README.md
│   │   ├── specs.py
│   │   └── validation.py
│   ├── ops/
│   │   ├── attention/
│   │   │   ├── benchmarks/
│   │   │   │   ├── bench_attention.py
│   │   │   │   └── BUILD.bazel
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_attention.py
│   │   │   ├── __init__.py
│   │   │   ├── api.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── dispatch.py
│   │   │   ├── flash.py
│   │   │   ├── paged.py
│   │   │   ├── pairformer.py
│   │   │   ├── ragged.py
│   │   │   ├── README.md
│   │   │   ├── reference.py
│   │   │   ├── registry.py
│   │   │   ├── triangle_attention.py
│   │   │   └── validation.py
│   │   ├── diffusion/
│   │   │   ├── benchmarks/
│   │   │   │   ├── bench_diffusion.py
│   │   │   │   └── BUILD.bazel
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_diffusion.py
│   │   │   ├── __init__.py
│   │   │   ├── api.py
│   │   │   ├── attention.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── dispatch.py
│   │   │   ├── modulation.py
│   │   │   ├── neighbor_attention.py
│   │   │   ├── preconditioning.py
│   │   │   ├── README.md
│   │   │   ├── reference.py
│   │   │   ├── registry.py
│   │   │   ├── sampling_step.py
│   │   │   └── validation.py
│   │   ├── fp8/
│   │   │   ├── benchmarks/
│   │   │   │   ├── bench_fp8.py
│   │   │   │   └── BUILD.bazel
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_fp8.py
│   │   │   ├── __init__.py
│   │   │   ├── api.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── casting.py
│   │   │   ├── dispatch.py
│   │   │   ├── formats.py
│   │   │   ├── gemm.py
│   │   │   ├── grouped_gemm.py
│   │   │   ├── linear.py
│   │   │   ├── README.md
│   │   │   ├── reference.py
│   │   │   ├── registry.py
│   │   │   ├── scaling.py
│   │   │   └── validation.py
│   │   ├── fused/
│   │   │   ├── benchmarks/
│   │   │   │   ├── bench_fused.py
│   │   │   │   └── BUILD.bazel
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_fused.py
│   │   │   ├── __init__.py
│   │   │   ├── adamw.py
│   │   │   ├── api.py
│   │   │   ├── bias_dropout_add.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── cross_entropy.py
│   │   │   ├── dispatch.py
│   │   │   ├── ema.py
│   │   │   ├── norms.py
│   │   │   ├── outer_product_mean.py
│   │   │   ├── README.md
│   │   │   ├── reference.py
│   │   │   ├── registry.py
│   │   │   ├── rotary.py
│   │   │   ├── segment_reduce.py
│   │   │   ├── swiglu.py
│   │   │   ├── triangle_multiplication.py
│   │   │   └── validation.py
│   │   └── moe/
│   │       ├── benchmarks/
│   │       │   ├── bench_moe.py
│   │       │   └── BUILD.bazel
│   │       ├── tests/
│   │       │   ├── __init__.py
│   │       │   ├── BUILD.bazel
│   │       │   └── test_moe.py
│   │       ├── __init__.py
│   │       ├── api.py
│   │       ├── BUILD.bazel
│   │       ├── capacity.py
│   │       ├── combine.py
│   │       ├── dispatch.py
│   │       ├── grouped_gemm.py
│   │       ├── permutation.py
│   │       ├── README.md
│   │       ├── reference.py
│   │       ├── registry.py
│   │       ├── router.py
│   │       ├── topk.py
│   │       └── validation.py
│   ├── providers/
│   │   ├── pytorch/
│   │   │   ├── attention/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── attention.py
│   │   │   │   └── BUILD.bazel
│   │   │   ├── diffusion/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── diffusion.py
│   │   │   ├── fp8/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── fp8.py
│   │   │   ├── fused/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── fused.py
│   │   │   ├── moe/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── moe.py
│   │   │   ├── __init__.py
│   │   │   ├── adapter.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── capabilities.py
│   │   │   ├── manifest.py
│   │   │   ├── policy.py
│   │   │   ├── README.md
│   │   │   └── registry.py
│   │   ├── tilelang/
│   │   │   ├── attention/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── attention.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── configs.py
│   │   │   │   └── schedules.py
│   │   │   ├── diffusion/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── configs.py
│   │   │   │   ├── diffusion.py
│   │   │   │   └── schedules.py
│   │   │   ├── fp8/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── configs.py
│   │   │   │   ├── fp8.py
│   │   │   │   └── schedules.py
│   │   │   ├── fused/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── configs.py
│   │   │   │   ├── fused.py
│   │   │   │   └── schedules.py
│   │   │   ├── moe/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── configs.py
│   │   │   │   ├── moe.py
│   │   │   │   └── schedules.py
│   │   │   ├── __init__.py
│   │   │   ├── adapter.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── capabilities.py
│   │   │   ├── manifest.py
│   │   │   ├── policy.py
│   │   │   ├── README.md
│   │   │   └── registry.py
│   │   └── vendor/
│   │       ├── __init__.py
│   │       ├── adapter.py
│   │       ├── BUILD.bazel
│   │       ├── capabilities.py
│   │       ├── manifest.py
│   │       ├── policy.py
│   │       ├── README.md
│   │       └── registry.py
│   ├── qualification/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_qualification.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── evidence.py
│   │   ├── fallback.py
│   │   ├── maturity.py
│   │   ├── numerical.py
│   │   ├── performance.py
│   │   ├── promotion.py
│   │   ├── README.md
│   │   └── revocation.py
│   ├── tilelang/
│   │   ├── autotune/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_autotune.py
│   │   │   ├── __init__.py
│   │   │   ├── budget.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── candidate.py
│   │   │   ├── database.py
│   │   │   ├── objective.py
│   │   │   ├── README.md
│   │   │   ├── reproducibility.py
│   │   │   ├── runner.py
│   │   │   ├── search_space.py
│   │   │   └── validation.py
│   │   ├── compiler/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── test_codegen.py
│   │   │   │   └── test_layouts.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── cache.py
│   │   │   ├── compiler.py
│   │   │   ├── diagnostics.py
│   │   │   ├── ir.py
│   │   │   ├── layouts.py
│   │   │   ├── lowering.py
│   │   │   ├── pipeline.py
│   │   │   ├── README.md
│   │   │   ├── runtime.py
│   │   │   ├── swizzle.py
│   │   │   ├── tma.py
│   │   │   ├── warp_specialization.py
│   │   │   └── wgmma.py
│   │   ├── targets/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_targets.py
│   │   │   ├── __init__.py
│   │   │   ├── amd_cdna.py
│   │   │   ├── blackwell.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── common.py
│   │   │   ├── hopper.py
│   │   │   └── README.md
│   │   └── testing/
│   │       ├── tests/
│   │       │   ├── __init__.py
│   │       │   ├── BUILD.bazel
│   │       │   └── test_harness.py
│   │       ├── __init__.py
│   │       ├── BUILD.bazel
│   │       ├── compile.py
│   │       ├── devices.py
│   │       ├── goldens.py
│   │       ├── numerics.py
│   │       ├── performance.py
│   │       └── README.md
│   ├── __init__.py
│   ├── api.py
│   ├── BUILD.bazel
│   ├── dispatch.py
│   ├── manifest.py
│   ├── README.md
│   ├── registry.py
│   └── version.py
├── libs/
│   ├── go/
│   │   ├── audit/
│   │   │   ├── action.go
│   │   │   ├── actor.go
│   │   │   ├── BUILD.bazel
│   │   │   ├── change.go
│   │   │   ├── contracts_test.go
│   │   │   ├── doc.go
│   │   │   ├── errors.go
│   │   │   ├── event.go
│   │   │   ├── event_test.go
│   │   │   ├── factory.go
│   │   │   ├── fields.go
│   │   │   ├── README.md
│   │   │   ├── recorder.go
│   │   │   ├── recorder_test.go
│   │   │   ├── target.go
│   │   │   └── validation.go
│   │   ├── auth/
│   │   │   ├── attributes.go
│   │   │   ├── authenticator.go
│   │   │   ├── authenticator_test.go
│   │   │   ├── authorizer.go
│   │   │   ├── authorizer_test.go
│   │   │   ├── BUILD.bazel
│   │   │   ├── claims.go
│   │   │   ├── claims_test.go
│   │   │   ├── context.go
│   │   │   ├── context_test.go
│   │   │   ├── contracts_test.go
│   │   │   ├── credential.go
│   │   │   ├── credential_test.go
│   │   │   ├── decision.go
│   │   │   ├── doc.go
│   │   │   ├── errors.go
│   │   │   ├── internal.go
│   │   │   ├── permission.go
│   │   │   ├── permission_test.go
│   │   │   ├── principal.go
│   │   │   ├── principal_test.go
│   │   │   ├── README.md
│   │   │   └── resource.go
│   │   ├── clock/
│   │   │   ├── BUILD.bazel
│   │   │   ├── clock.go
│   │   │   ├── clock_test.go
│   │   │   ├── doc.go
│   │   │   ├── errors.go
│   │   │   ├── fake.go
│   │   │   ├── fake_test.go
│   │   │   ├── README.md
│   │   │   └── real.go
│   │   ├── connectx/
│   │   │   ├── connecttest/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── README.md
│   │   │   │   ├── recorder.go
│   │   │   │   ├── recorder_test.go
│   │   │   │   ├── server.go
│   │   │   │   └── server_test.go
│   │   │   ├── health/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── checker.go
│   │   │   │   ├── checker_test.go
│   │   │   │   ├── doc.go
│   │   │   │   ├── handler.go
│   │   │   │   └── README.md
│   │   │   ├── interceptors/
│   │   │   │   ├── authentication.go
│   │   │   │   ├── authorization.go
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── faults.go
│   │   │   │   ├── interceptors_test.go
│   │   │   │   ├── internal.go
│   │   │   │   ├── README.md
│   │   │   │   ├── recovery.go
│   │   │   │   ├── requestmeta.go
│   │   │   │   ├── stack.go
│   │   │   │   ├── types.go
│   │   │   │   └── validation.go
│   │   │   ├── otel/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── otel.go
│   │   │   │   ├── otel_test.go
│   │   │   │   └── README.md
│   │   │   ├── reflection/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── handlers.go
│   │   │   │   ├── handlers_test.go
│   │   │   │   └── README.md
│   │   │   ├── BUILD.bazel
│   │   │   ├── codes.go
│   │   │   ├── codes_test.go
│   │   │   ├── config.go
│   │   │   ├── config_test.go
│   │   │   ├── details.go
│   │   │   ├── doc.go
│   │   │   ├── errors.go
│   │   │   ├── faults.go
│   │   │   ├── faults_test.go
│   │   │   ├── headers.go
│   │   │   ├── metadata.go
│   │   │   ├── mount.go
│   │   │   ├── mount_test.go
│   │   │   ├── procedure.go
│   │   │   └── README.md
│   │   ├── faults/
│   │   │   ├── BUILD.bazel
│   │   │   ├── classify.go
│   │   │   ├── classify_test.go
│   │   │   ├── code.go
│   │   │   ├── code_test.go
│   │   │   ├── context.go
│   │   │   ├── context_test.go
│   │   │   ├── doc.go
│   │   │   ├── fault.go
│   │   │   ├── fault_test.go
│   │   │   ├── fields.go
│   │   │   ├── fields_test.go
│   │   │   ├── options.go
│   │   │   ├── README.md
│   │   │   ├── retry.go
│   │   │   └── retry_test.go
│   │   ├── grpcx/
│   │   │   ├── credentials/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── README.md
│   │   │   │   ├── tls.go
│   │   │   │   └── tls_test.go
│   │   │   ├── grpctest/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── harness.go
│   │   │   │   ├── harness_test.go
│   │   │   │   └── README.md
│   │   │   ├── health/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── README.md
│   │   │   │   ├── synchronizer.go
│   │   │   │   └── synchronizer_test.go
│   │   │   ├── interceptors/
│   │   │   │   ├── authentication.go
│   │   │   │   ├── authorization.go
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── config.go
│   │   │   │   ├── doc.go
│   │   │   │   ├── faults.go
│   │   │   │   ├── full_test.go
│   │   │   │   ├── interceptors_test.go
│   │   │   │   ├── internal.go
│   │   │   │   ├── README.md
│   │   │   │   ├── recovery.go
│   │   │   │   ├── requestmeta.go
│   │   │   │   ├── stream.go
│   │   │   │   └── validation.go
│   │   │   ├── otel/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── otel.go
│   │   │   │   ├── otel_test.go
│   │   │   │   └── README.md
│   │   │   ├── reflection/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── README.md
│   │   │   │   ├── register.go
│   │   │   │   └── register_test.go
│   │   │   ├── BUILD.bazel
│   │   │   ├── client.go
│   │   │   ├── codes.go
│   │   │   ├── codes_test.go
│   │   │   ├── config.go
│   │   │   ├── config_test.go
│   │   │   ├── details.go
│   │   │   ├── details_test.go
│   │   │   ├── doc.go
│   │   │   ├── errors.go
│   │   │   ├── internal.go
│   │   │   ├── metadata.go
│   │   │   ├── metadata_test.go
│   │   │   ├── method.go
│   │   │   ├── method_test.go
│   │   │   ├── README.md
│   │   │   ├── server.go
│   │   │   └── server_test.go
│   │   ├── httpx/
│   │   │   ├── health/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── handler.go
│   │   │   │   ├── handler_test.go
│   │   │   │   └── README.md
│   │   │   ├── httpxtest/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── problem.go
│   │   │   │   ├── problem_test.go
│   │   │   │   ├── README.md
│   │   │   │   ├── server.go
│   │   │   │   └── server_test.go
│   │   │   ├── middleware/
│   │   │   │   ├── access.go
│   │   │   │   ├── authentication.go
│   │   │   │   ├── authorization.go
│   │   │   │   ├── body.go
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── chain.go
│   │   │   │   ├── doc.go
│   │   │   │   ├── internal.go
│   │   │   │   ├── middleware_test.go
│   │   │   │   ├── README.md
│   │   │   │   ├── recovery.go
│   │   │   │   ├── requestmeta.go
│   │   │   │   ├── security.go
│   │   │   │   ├── stack.go
│   │   │   │   └── writer.go
│   │   │   ├── otel/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── otel.go
│   │   │   │   ├── otel_test.go
│   │   │   │   └── README.md
│   │   │   ├── BUILD.bazel
│   │   │   ├── client.go
│   │   │   ├── client_test.go
│   │   │   ├── codes.go
│   │   │   ├── codes_test.go
│   │   │   ├── doc.go
│   │   │   ├── errors.go
│   │   │   ├── headers.go
│   │   │   ├── json.go
│   │   │   ├── json_test.go
│   │   │   ├── problem.go
│   │   │   ├── problem_test.go
│   │   │   ├── propagation.go
│   │   │   ├── propagation_test.go
│   │   │   ├── README.md
│   │   │   ├── server.go
│   │   │   └── server_test.go
│   │   ├── idempotency/
│   │   │   ├── idempotencytest/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── contracts_test.go
│   │   │   │   ├── doc.go
│   │   │   │   ├── memory.go
│   │   │   │   ├── memory_test.go
│   │   │   │   └── README.md
│   │   │   ├── BUILD.bazel
│   │   │   ├── contracts_test.go
│   │   │   ├── doc.go
│   │   │   ├── errors.go
│   │   │   ├── executor.go
│   │   │   ├── executor_contracts_test.go
│   │   │   ├── executor_test.go
│   │   │   ├── internal.go
│   │   │   ├── key.go
│   │   │   ├── key_test.go
│   │   │   ├── README.md
│   │   │   ├── record.go
│   │   │   ├── record_test.go
│   │   │   ├── result.go
│   │   │   ├── result_test.go
│   │   │   └── store.go
│   │   ├── identifiers/
│   │   │   ├── api_test.go
│   │   │   ├── BUILD.bazel
│   │   │   ├── digest.go
│   │   │   ├── digest_test.go
│   │   │   ├── doc.go
│   │   │   ├── errors.go
│   │   │   ├── errors_test.go
│   │   │   ├── generator.go
│   │   │   ├── generator_test.go
│   │   │   ├── id.go
│   │   │   ├── id_test.go
│   │   │   ├── kind.go
│   │   │   ├── kind_test.go
│   │   │   ├── README.md
│   │   │   ├── uuid.go
│   │   │   └── uuid_test.go
│   │   ├── internal/
│   │   │   └── rpcfaults/
│   │   │       ├── BUILD.bazel
│   │   │       ├── details.go
│   │   │       ├── details_test.go
│   │   │       ├── doc.go
│   │   │       └── README.md
│   │   ├── kubernetes/
│   │   │   ├── client/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── client.go
│   │   │   │   ├── client_test.go
│   │   │   │   ├── config.go
│   │   │   │   ├── config_test.go
│   │   │   │   ├── discovery.go
│   │   │   │   ├── doc.go
│   │   │   │   ├── factory.go
│   │   │   │   └── README.md
│   │   │   ├── conditions/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── conditions.go
│   │   │   │   ├── conditions_test.go
│   │   │   │   ├── doc.go
│   │   │   │   └── README.md
│   │   │   ├── controller/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── controller_test.go
│   │   │   │   ├── doc.go
│   │   │   │   ├── fields.go
│   │   │   │   ├── observer.go
│   │   │   │   ├── options.go
│   │   │   │   ├── qualification.go
│   │   │   │   ├── README.md
│   │   │   │   ├── reconciler.go
│   │   │   │   ├── recovery.go
│   │   │   │   └── requestmeta.go
│   │   │   ├── events/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── README.md
│   │   │   │   ├── recorder.go
│   │   │   │   └── recorder_test.go
│   │   │   ├── finalizers/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── finalizers.go
│   │   │   │   ├── finalizers_test.go
│   │   │   │   └── README.md
│   │   │   ├── metadata/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── metadata.go
│   │   │   │   ├── metadata_test.go
│   │   │   │   └── README.md
│   │   │   ├── ownerrefs/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── ownerrefs.go
│   │   │   │   ├── ownerrefs_test.go
│   │   │   │   └── README.md
│   │   │   ├── patch/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── patch.go
│   │   │   │   ├── patch_test.go
│   │   │   │   └── README.md
│   │   │   ├── status/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── README.md
│   │   │   │   ├── status.go
│   │   │   │   └── status_test.go
│   │   │   ├── watch/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── README.md
│   │   │   │   ├── watch.go
│   │   │   │   └── watch_test.go
│   │   │   ├── BUILD.bazel
│   │   │   ├── doc.go
│   │   │   ├── errors.go
│   │   │   ├── errors_test.go
│   │   │   ├── README.md
│   │   │   ├── reference.go
│   │   │   └── reference_test.go
│   │   ├── observability/
│   │   │   ├── obstest/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── doc.go
│   │   │   │   ├── errors.go
│   │   │   │   ├── logging.go
│   │   │   │   ├── metrics.go
│   │   │   │   ├── obstest_test.go
│   │   │   │   ├── README.md
│   │   │   │   └── tracing.go
│   │   │   ├── attributes.go
│   │   │   ├── attributes_test.go
│   │   │   ├── BUILD.bazel
│   │   │   ├── contracts_test.go
│   │   │   ├── doc.go
│   │   │   ├── errors.go
│   │   │   ├── handler.go
│   │   │   ├── internal.go
│   │   │   ├── lifecycle.go
│   │   │   ├── lifecycle_test.go
│   │   │   ├── logging.go
│   │   │   ├── logging_test.go
│   │   │   ├── metrics.go
│   │   │   ├── metrics_test.go
│   │   │   ├── propagation.go
│   │   │   ├── propagation_test.go
│   │   │   ├── README.md
│   │   │   ├── resource.go
│   │   │   ├── resource_test.go
│   │   │   ├── runtime.go
│   │   │   ├── runtime_test.go
│   │   │   ├── tracing.go
│   │   │   └── tracing_test.go
│   │   ├── requestmeta/
│   │   │   ├── BUILD.bazel
│   │   │   ├── context.go
│   │   │   ├── context_test.go
│   │   │   ├── contracts_test.go
│   │   │   ├── doc.go
│   │   │   ├── errors.go
│   │   │   ├── metadata.go
│   │   │   ├── metadata_test.go
│   │   │   ├── operation.go
│   │   │   ├── propagation.go
│   │   │   ├── propagation_test.go
│   │   │   ├── README.md
│   │   │   ├── request_id.go
│   │   │   ├── request_id_test.go
│   │   │   └── token.go
│   │   ├── retry/
│   │   │   ├── attempt.go
│   │   │   ├── backoff.go
│   │   │   ├── backoff_test.go
│   │   │   ├── BUILD.bazel
│   │   │   ├── contracts_test.go
│   │   │   ├── doc.go
│   │   │   ├── errors.go
│   │   │   ├── executor.go
│   │   │   ├── executor_test.go
│   │   │   ├── jitter.go
│   │   │   ├── observer.go
│   │   │   ├── observer_test.go
│   │   │   ├── options.go
│   │   │   ├── policy.go
│   │   │   ├── policy_test.go
│   │   │   ├── README.md
│   │   │   └── result.go
│   │   ├── servicekit/
│   │   │   ├── BUILD.bazel
│   │   │   ├── build.go
│   │   │   ├── build_test.go
│   │   │   ├── clock_test.go
│   │   │   ├── component.go
│   │   │   ├── component_test.go
│   │   │   ├── doc.go
│   │   │   ├── errors.go
│   │   │   ├── errors_test.go
│   │   │   ├── event.go
│   │   │   ├── event_test.go
│   │   │   ├── faults.go
│   │   │   ├── internal.go
│   │   │   ├── options.go
│   │   │   ├── options_test.go
│   │   │   ├── probe.go
│   │   │   ├── probe_test.go
│   │   │   ├── README.md
│   │   │   ├── service.go
│   │   │   ├── service_test.go
│   │   │   ├── shutdown.go
│   │   │   ├── signal.go
│   │   │   ├── signal_test.go
│   │   │   ├── signal_unix.go
│   │   │   ├── signal_windows.go
│   │   │   ├── state.go
│   │   │   ├── state_test.go
│   │   │   └── timeout.go
│   │   ├── storage/
│   │   │   ├── blob/
│   │   │   │   ├── blobtest/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── doc.go
│   │   │   │   │   ├── README.md
│   │   │   │   │   └── suite.go
│   │   │   │   ├── gcs/
│   │   │   │   │   ├── attributes_test.go
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── doc.go
│   │   │   │   │   ├── errors.go
│   │   │   │   │   ├── errors_test.go
│   │   │   │   │   ├── options.go
│   │   │   │   │   ├── options_test.go
│   │   │   │   │   ├── README.md
│   │   │   │   │   ├── spool.go
│   │   │   │   │   ├── spool_test.go
│   │   │   │   │   ├── store.go
│   │   │   │   │   └── store_test.go
│   │   │   │   ├── memory/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── conformance_test.go
│   │   │   │   │   ├── doc.go
│   │   │   │   │   ├── README.md
│   │   │   │   │   ├── store.go
│   │   │   │   │   └── store_test.go
│   │   │   │   ├── attributes.go
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── contracts_test.go
│   │   │   │   ├── doc.go
│   │   │   │   ├── errors.go
│   │   │   │   ├── key.go
│   │   │   │   ├── key_test.go
│   │   │   │   ├── metadata.go
│   │   │   │   ├── options.go
│   │   │   │   ├── options_test.go
│   │   │   │   ├── README.md
│   │   │   │   └── store.go
│   │   │   ├── cache/
│   │   │   │   ├── cachetest/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── conformance.go
│   │   │   │   │   ├── doc.go
│   │   │   │   │   └── README.md
│   │   │   │   ├── memory/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── conformance_test.go
│   │   │   │   │   ├── doc.go
│   │   │   │   │   ├── README.md
│   │   │   │   │   ├── store.go
│   │   │   │   │   └── store_test.go
│   │   │   │   ├── redis/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── doc.go
│   │   │   │   │   ├── options.go
│   │   │   │   │   ├── options_test.go
│   │   │   │   │   ├── README.md
│   │   │   │   │   ├── scripts.go
│   │   │   │   │   ├── store.go
│   │   │   │   │   └── store_test.go
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── contracts_test.go
│   │   │   │   ├── doc.go
│   │   │   │   ├── entry.go
│   │   │   │   ├── errors.go
│   │   │   │   ├── internal.go
│   │   │   │   ├── key.go
│   │   │   │   ├── options.go
│   │   │   │   ├── README.md
│   │   │   │   └── store.go
│   │   │   ├── lease/
│   │   │   │   ├── leasetest/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── conformance.go
│   │   │   │   │   ├── doc.go
│   │   │   │   │   └── README.md
│   │   │   │   ├── memory/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── conformance_test.go
│   │   │   │   │   ├── doc.go
│   │   │   │   │   ├── README.md
│   │   │   │   │   ├── store.go
│   │   │   │   │   └── store_test.go
│   │   │   │   ├── postgres/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── doc.go
│   │   │   │   │   ├── README.md
│   │   │   │   │   ├── store.go
│   │   │   │   │   └── store_test.go
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── contracts_test.go
│   │   │   │   ├── doc.go
│   │   │   │   ├── errors.go
│   │   │   │   ├── key.go
│   │   │   │   ├── lease.go
│   │   │   │   ├── README.md
│   │   │   │   └── store.go
│   │   │   ├── outbox/
│   │   │   │   ├── outboxtest/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── conformance.go
│   │   │   │   │   ├── memory.go
│   │   │   │   │   ├── memory_test.go
│   │   │   │   │   └── README.md
│   │   │   │   ├── postgres/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── README.md
│   │   │   │   │   ├── repository.go
│   │   │   │   │   ├── repository_test.go
│   │   │   │   │   ├── scanner.go
│   │   │   │   │   └── transaction.go
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── contracts_test.go
│   │   │   │   ├── dispatcher.go
│   │   │   │   ├── doc.go
│   │   │   │   ├── envelope.go
│   │   │   │   ├── errors.go
│   │   │   │   ├── README.md
│   │   │   │   ├── record.go
│   │   │   │   ├── repository.go
│   │   │   │   └── status.go
│   │   │   ├── sql/
│   │   │   │   ├── postgres/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── config.go
│   │   │   │   │   ├── config_test.go
│   │   │   │   │   ├── doc.go
│   │   │   │   │   ├── errors.go
│   │   │   │   │   ├── errors_test.go
│   │   │   │   │   └── README.md
│   │   │   │   ├── sqltest/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── doc.go
│   │   │   │   │   ├── driver.go
│   │   │   │   │   └── driver_test.go
│   │   │   │   ├── transaction/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── context.go
│   │   │   │   │   ├── doc.go
│   │   │   │   │   ├── errors.go
│   │   │   │   │   ├── README.md
│   │   │   │   │   ├── run.go
│   │   │   │   │   └── run_test.go
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── README.md
│   │   │   ├── BUILD.bazel
│   │   │   └── README.md
│   │   ├── BUILD.bazel
│   │   ├── LAYERS.md
│   │   └── README.md
│   ├── python/
│   │   ├── artifacts/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── test_manifest.py
│   │   │   │   └── test_verification.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── client.py
│   │   │   ├── lineage.py
│   │   │   ├── manifest.py
│   │   │   ├── README.md
│   │   │   ├── reference.py
│   │   │   └── verification.py
│   │   ├── config/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── test_loader.py
│   │   │   │   └── test_merge.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── fingerprint.py
│   │   │   ├── loader.py
│   │   │   ├── merge.py
│   │   │   ├── overrides.py
│   │   │   ├── README.md
│   │   │   ├── schema.py
│   │   │   └── validation.py
│   │   ├── distributed/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_topology.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── environment.py
│   │   │   ├── health.py
│   │   │   ├── README.md
│   │   │   ├── rendezvous.py
│   │   │   └── topology.py
│   │   ├── errors/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_codes.py
│   │   │   ├── __init__.py
│   │   │   ├── base.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── codes.py
│   │   │   ├── README.md
│   │   │   └── retry.py
│   │   ├── geometry/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_rigid.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── distances.py
│   │   │   ├── frames.py
│   │   │   ├── invariants.py
│   │   │   ├── README.md
│   │   │   ├── rigid.py
│   │   │   └── transforms.py
│   │   ├── identifiers/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_digest.py
│   │   │   ├── __init__.py
│   │   │   ├── aliases.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── digest.py
│   │   │   ├── README.md
│   │   │   ├── resolver.py
│   │   │   └── uri.py
│   │   ├── observability/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_redaction.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── logging.py
│   │   │   ├── metrics.py
│   │   │   ├── README.md
│   │   │   ├── redaction.py
│   │   │   └── tracing.py
│   │   ├── serialization/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_canonical.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── canonical.py
│   │   │   ├── json.py
│   │   │   ├── protobuf.py
│   │   │   ├── README.md
│   │   │   ├── toml.py
│   │   │   └── yaml.py
│   │   ├── testing/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_numerics.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── devices.py
│   │   │   ├── distributed.py
│   │   │   ├── fixtures.py
│   │   │   ├── numerics.py
│   │   │   ├── processes.py
│   │   │   └── README.md
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   └── README.md
│   ├── rust/
│   │   ├── artifact_cas/
│   │   │   ├── src/
│   │   │   │   ├── blob.rs
│   │   │   │   ├── gc.rs
│   │   │   │   ├── index.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── manifest.rs
│   │   │   │   ├── retention.rs
│   │   │   │   └── store.rs
│   │   │   ├── tests/
│   │   │   │   ├── corruption.rs
│   │   │   │   └── integration.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── bio_formats/
│   │   │   ├── corpus/
│   │   │   │   └── README.md
│   │   │   ├── fuzz/
│   │   │   │   └── README.md
│   │   │   ├── src/
│   │   │   │   ├── a3m/
│   │   │   │   │   ├── mod.rs
│   │   │   │   │   ├── parser.rs
│   │   │   │   │   ├── record.rs
│   │   │   │   │   └── serializer.rs
│   │   │   │   ├── fasta/
│   │   │   │   │   ├── mod.rs
│   │   │   │   │   ├── parser.rs
│   │   │   │   │   ├── record.rs
│   │   │   │   │   └── serializer.rs
│   │   │   │   ├── fastq/
│   │   │   │   │   ├── mod.rs
│   │   │   │   │   ├── parser.rs
│   │   │   │   │   ├── record.rs
│   │   │   │   │   └── serializer.rs
│   │   │   │   ├── mmcif/
│   │   │   │   │   ├── lexer.rs
│   │   │   │   │   ├── mod.rs
│   │   │   │   │   ├── parser.rs
│   │   │   │   │   ├── record.rs
│   │   │   │   │   └── serializer.rs
│   │   │   │   ├── mol/
│   │   │   │   │   ├── mod.rs
│   │   │   │   │   ├── parser.rs
│   │   │   │   │   ├── record.rs
│   │   │   │   │   └── serializer.rs
│   │   │   │   ├── pdb/
│   │   │   │   │   ├── mod.rs
│   │   │   │   │   ├── parser.rs
│   │   │   │   │   ├── record.rs
│   │   │   │   │   └── serializer.rs
│   │   │   │   ├── sdf/
│   │   │   │   │   ├── mod.rs
│   │   │   │   │   ├── parser.rs
│   │   │   │   │   ├── record.rs
│   │   │   │   │   └── serializer.rs
│   │   │   │   ├── stockholm/
│   │   │   │   │   ├── mod.rs
│   │   │   │   │   ├── parser.rs
│   │   │   │   │   ├── record.rs
│   │   │   │   │   └── serializer.rs
│   │   │   │   ├── common.rs
│   │   │   │   └── lib.rs
│   │   │   ├── tests/
│   │   │   │   └── roundtrip.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── bounded_parse/
│   │   │   ├── fuzz/
│   │   │   │   └── README.md
│   │   │   ├── src/
│   │   │   │   ├── budget.rs
│   │   │   │   ├── cursor.rs
│   │   │   │   ├── diagnostic.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── limits.rs
│   │   │   │   ├── location.rs
│   │   │   │   ├── mode.rs
│   │   │   │   ├── recovery.rs
│   │   │   │   └── source.rs
│   │   │   ├── tests/
│   │   │   │   ├── allocation_limits.rs
│   │   │   │   ├── nesting_limits.rs
│   │   │   │   └── truncation.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── bytes_io/
│   │   │   ├── src/
│   │   │   │   ├── alignment.rs
│   │   │   │   ├── buffer.rs
│   │   │   │   ├── copy.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── metrics.rs
│   │   │   │   ├── pool.rs
│   │   │   │   ├── range.rs
│   │   │   │   └── vectored.rs
│   │   │   ├── tests/
│   │   │   │   ├── pool.rs
│   │   │   │   └── range.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── checkpoint_io/
│   │   │   ├── src/
│   │   │   │   ├── lib.rs
│   │   │   │   ├── manifest.rs
│   │   │   │   ├── reader.rs
│   │   │   │   ├── repair.rs
│   │   │   │   ├── staging.rs
│   │   │   │   ├── verify.rs
│   │   │   │   └── writer.rs
│   │   │   ├── tests/
│   │   │   │   ├── fault_injection.rs
│   │   │   │   └── integration.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── content_digest/
│   │   │   ├── src/
│   │   │   │   ├── algorithm.rs
│   │   │   │   ├── digest.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── reader.rs
│   │   │   │   └── writer.rs
│   │   │   ├── tests/
│   │   │   │   └── vectors.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── data_stream/
│   │   │   ├── src/
│   │   │   │   ├── cache.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── metrics.rs
│   │   │   │   ├── prefetch.rs
│   │   │   │   ├── ranges.rs
│   │   │   │   ├── reader.rs
│   │   │   │   ├── resume.rs
│   │   │   │   └── shuffle.rs
│   │   │   ├── tests/
│   │   │   │   ├── integration.rs
│   │   │   │   └── resume.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── faults/
│   │   │   ├── src/
│   │   │   │   ├── code.rs
│   │   │   │   ├── context.rs
│   │   │   │   ├── fault.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── retry.rs
│   │   │   │   └── wire.rs
│   │   │   ├── tests/
│   │   │   │   └── conformance.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── gpu_host/
│   │   │   ├── src/
│   │   │   │   ├── providers/
│   │   │   │   │   ├── amd.rs
│   │   │   │   │   ├── mod.rs
│   │   │   │   │   └── nvidia.rs
│   │   │   │   ├── budget.rs
│   │   │   │   ├── device.rs
│   │   │   │   ├── inventory.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── memory.rs
│   │   │   │   └── process.rs
│   │   │   ├── tests/
│   │   │   │   ├── budget.rs
│   │   │   │   └── inventory.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── identifiers/
│   │   │   ├── src/
│   │   │   │   ├── digest.rs
│   │   │   │   ├── id.rs
│   │   │   │   ├── kind.rs
│   │   │   │   ├── lib.rs
│   │   │   │   └── resource_version.rs
│   │   │   ├── tests/
│   │   │   │   └── goldens.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── ipc/
│   │   │   ├── src/
│   │   │   │   ├── control.rs
│   │   │   │   ├── descriptor.rs
│   │   │   │   ├── framing.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── shared_memory.rs
│   │   │   │   ├── unix.rs
│   │   │   │   └── windows.rs
│   │   │   ├── tests/
│   │   │   │   ├── framing.rs
│   │   │   │   └── lifetime.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   ├── README.md
│   │   │   └── SAFETY.md
│   │   ├── manifests/
│   │   │   ├── src/
│   │   │   │   ├── artifact.rs
│   │   │   │   ├── checkpoint.rs
│   │   │   │   ├── dataset.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── runtime.rs
│   │   │   │   ├── tensor.rs
│   │   │   │   └── validation.rs
│   │   │   ├── tests/
│   │   │   │   └── roundtrip.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── object_store/
│   │   │   ├── src/
│   │   │   │   ├── adapters/
│   │   │   │   │   ├── arrow.rs
│   │   │   │   │   ├── local.rs
│   │   │   │   │   ├── memory.rs
│   │   │   │   │   └── mod.rs
│   │   │   │   ├── client.rs
│   │   │   │   ├── conditional.rs
│   │   │   │   ├── config.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── metrics.rs
│   │   │   │   ├── multipart.rs
│   │   │   │   ├── namespace.rs
│   │   │   │   ├── range.rs
│   │   │   │   ├── retry.rs
│   │   │   │   └── verification.rs
│   │   │   ├── tests/
│   │   │   │   ├── conditional.rs
│   │   │   │   ├── conformance.rs
│   │   │   │   ├── failure_injection.rs
│   │   │   │   └── range.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── python_bridge/
│   │   │   ├── python/
│   │   │   │   └── mindclade_native/
│   │   │   │       ├── __init__.py
│   │   │   │       └── py.typed
│   │   │   ├── src/
│   │   │   │   ├── buffers.rs
│   │   │   │   ├── errors.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── manifests.rs
│   │   │   │   ├── parsers.rs
│   │   │   │   └── tokenizers.rs
│   │   │   ├── tests/
│   │   │   │   └── python_api.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   ├── pyproject.toml
│   │   │   └── README.md
│   │   ├── record_io/
│   │   │   ├── src/
│   │   │   │   ├── compression.rs
│   │   │   │   ├── frame.rs
│   │   │   │   ├── index.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── reader.rs
│   │   │   │   └── writer.rs
│   │   │   ├── tests/
│   │   │   │   ├── frame.rs
│   │   │   │   └── recovery.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── runtime_core/
│   │   │   ├── src/
│   │   │   │   ├── budget/
│   │   │   │   │   ├── account.rs
│   │   │   │   │   ├── allocation.rs
│   │   │   │   │   ├── hierarchy.rs
│   │   │   │   │   ├── limits.rs
│   │   │   │   │   ├── mod.rs
│   │   │   │   │   ├── reservation.rs
│   │   │   │   │   ├── snapshot.rs
│   │   │   │   │   └── tracker.rs
│   │   │   │   ├── cancellation.rs
│   │   │   │   ├── clock.rs
│   │   │   │   ├── deadline.rs
│   │   │   │   ├── fencing.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── retry.rs
│   │   │   │   └── task_group.rs
│   │   │   ├── tests/
│   │   │   │   ├── budget.rs
│   │   │   │   ├── cancellation.rs
│   │   │   │   └── task_group.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── servicekit/
│   │   │   ├── src/
│   │   │   │   ├── config.rs
│   │   │   │   ├── health.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── server.rs
│   │   │   │   ├── shutdown.rs
│   │   │   │   └── signals.rs
│   │   │   ├── tests/
│   │   │   │   └── lifecycle.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── telemetry/
│   │   │   ├── src/
│   │   │   │   ├── attributes.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── logging.rs
│   │   │   │   ├── metrics.rs
│   │   │   │   ├── propagation.rs
│   │   │   │   └── tracing.rs
│   │   │   ├── tests/
│   │   │   │   └── redaction.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── telemetry_spool/
│   │   │   ├── src/
│   │   │   │   ├── batch.rs
│   │   │   │   ├── delivery.rs
│   │   │   │   ├── envelope.rs
│   │   │   │   ├── journal.rs
│   │   │   │   ├── lib.rs
│   │   │   │   └── spool.rs
│   │   │   ├── tests/
│   │   │   │   ├── limits.rs
│   │   │   │   └── recovery.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── worker_protocol/
│   │   │   ├── src/
│   │   │   │   ├── command.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── sequence.rs
│   │   │   │   ├── status.rs
│   │   │   │   ├── ticket.rs
│   │   │   │   └── validation.rs
│   │   │   ├── tests/
│   │   │   │   └── goldens.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── worker_runtime/
│   │   │   ├── src/
│   │   │   │   ├── commit.rs
│   │   │   │   ├── diagnostics.rs
│   │   │   │   ├── drain.rs
│   │   │   │   ├── heartbeat.rs
│   │   │   │   ├── lease.rs
│   │   │   │   ├── lib.rs
│   │   │   │   ├── machine.rs
│   │   │   │   ├── preemption.rs
│   │   │   │   ├── state.rs
│   │   │   │   └── supervisor.rs
│   │   │   ├── tests/
│   │   │   │   ├── fencing.rs
│   │   │   │   ├── shutdown.rs
│   │   │   │   └── state_machine.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   └── README.md
│   │   ├── BUILD.bazel
│   │   └── README.md
│   └── ts/
│       ├── api_client/
│       │   ├── src/
│       │   │   ├── client.ts
│       │   │   ├── errors.ts
│       │   │   ├── events.ts
│       │   │   ├── index.ts
│       │   │   ├── pagination.ts
│       │   │   └── types.ts
│       │   ├── BUILD.bazel
│       │   ├── package.json
│       │   ├── README.md
│       │   └── tsconfig.json
│       ├── auth/
│       │   ├── src/
│       │   │   ├── client.ts
│       │   │   ├── guards.tsx
│       │   │   ├── index.ts
│       │   │   ├── session.ts
│       │   │   └── types.ts
│       │   ├── BUILD.bazel
│       │   ├── package.json
│       │   ├── README.md
│       │   └── tsconfig.json
│       ├── charts/
│       │   ├── src/
│       │   │   ├── Heatmap.tsx
│       │   │   ├── Histogram.tsx
│       │   │   ├── index.ts
│       │   │   ├── LineChart.tsx
│       │   │   └── TopologyGraph.tsx
│       │   ├── BUILD.bazel
│       │   ├── package.json
│       │   ├── README.md
│       │   └── tsconfig.json
│       ├── design_system/
│       │   ├── src/
│       │   │   ├── Button.tsx
│       │   │   ├── DataTable.tsx
│       │   │   ├── index.ts
│       │   │   ├── Metric.tsx
│       │   │   ├── StatusBadge.tsx
│       │   │   └── theme.ts
│       │   ├── BUILD.bazel
│       │   ├── package.json
│       │   ├── README.md
│       │   └── tsconfig.json
│       ├── molecular_viewer/
│       │   ├── src/
│       │   │   ├── index.ts
│       │   │   ├── MolecularViewer.tsx
│       │   │   ├── Selection.ts
│       │   │   └── StructureLoader.ts
│       │   ├── BUILD.bazel
│       │   ├── package.json
│       │   ├── README.md
│       │   └── tsconfig.json
│       ├── telemetry/
│       │   ├── src/
│       │   │   ├── client.ts
│       │   │   ├── events.ts
│       │   │   ├── index.ts
│       │   │   └── redaction.ts
│       │   ├── BUILD.bazel
│       │   ├── package.json
│       │   ├── README.md
│       │   └── tsconfig.json
│       ├── BUILD.bazel
│       ├── package.json
│       ├── README.md
│       └── tsconfig.json
├── models/
│   ├── adapters/
│   │   ├── export/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_export.py
│   │   │   ├── __init__.py
│   │   │   ├── aot_inductor.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── onnx.py
│   │   │   ├── README.md
│   │   │   ├── torch_export.py
│   │   │   └── validation.py
│   │   ├── huggingface/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_huggingface.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── config.py
│   │   │   ├── modeling.py
│   │   │   ├── processing.py
│   │   │   ├── README.md
│   │   │   └── weights.py
│   │   └── serving/
│   │       ├── tests/
│   │       │   ├── __init__.py
│   │       │   ├── BUILD.bazel
│   │       │   └── test_serving.py
│   │       ├── __init__.py
│   │       ├── BUILD.bazel
│   │       ├── bundle.py
│   │       ├── loader.py
│   │       ├── manifest.py
│   │       ├── README.md
│   │       └── validation.py
│   ├── components/
│   │   ├── attention/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_attention.py
│   │   │   ├── __init__.py
│   │   │   ├── api.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── dense.py
│   │   │   ├── README.md
│   │   │   ├── rotary.py
│   │   │   └── sparse.py
│   │   ├── embeddings/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_embeddings.py
│   │   │   ├── __init__.py
│   │   │   ├── atom.py
│   │   │   ├── bond.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── msa.py
│   │   │   ├── pair.py
│   │   │   ├── README.md
│   │   │   ├── sequence.py
│   │   │   ├── template.py
│   │   │   └── token.py
│   │   ├── geometry/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_geometry.py
│   │   │   ├── __init__.py
│   │   │   ├── atom_layout.py
│   │   │   ├── bond_graph.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── chemistry.py
│   │   │   ├── README.md
│   │   │   ├── rigid.py
│   │   │   └── stereochemistry.py
│   │   ├── losses/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_losses.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── classification.py
│   │   │   ├── diffusion.py
│   │   │   ├── ranking.py
│   │   │   ├── README.md
│   │   │   └── structure.py
│   │   ├── nn/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_nn.py
│   │   │   ├── __init__.py
│   │   │   ├── activations.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── dropout.py
│   │   │   ├── feed_forward.py
│   │   │   ├── parametrization.py
│   │   │   ├── README.md
│   │   │   ├── residual.py
│   │   │   └── stochastic_depth.py
│   │   └── normalization/
│   │       ├── tests/
│   │       │   ├── __init__.py
│   │       │   ├── BUILD.bazel
│   │       │   └── test_normalization.py
│   │       ├── __init__.py
│   │       ├── BUILD.bazel
│   │       ├── layer_norm.py
│   │       ├── README.md
│   │       └── rms_norm.py
│   ├── contracts/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_contracts.py
│   │   ├── __init__.py
│   │   ├── artifacts.py
│   │   ├── BUILD.bazel
│   │   ├── checkpoint.py
│   │   ├── compatibility.py
│   │   ├── compile_plan.py
│   │   ├── config.py
│   │   ├── export.py
│   │   ├── initialization.py
│   │   ├── model.py
│   │   ├── outputs.py
│   │   ├── parallel_plan.py
│   │   ├── provenance.py
│   │   ├── README.md
│   │   ├── state.py
│   │   └── weight_manifest.py
│   ├── families/
│   │   ├── biology/
│   │   │   ├── clade_1/
│   │   │   │   ├── heads/
│   │   │   │   │   ├── __init__.py
│   │   │   │   │   ├── affinity.py
│   │   │   │   │   ├── confidence.py
│   │   │   │   │   └── interface.py
│   │   │   │   ├── reference/
│   │   │   │   │   ├── tiny.py
│   │   │   │   │   └── tiny.toml
│   │   │   │   ├── structure/
│   │   │   │   │   ├── __init__.py
│   │   │   │   │   ├── atom_attention.py
│   │   │   │   │   ├── diffusion_module.py
│   │   │   │   │   ├── noise_schedule.py
│   │   │   │   │   └── sampler.py
│   │   │   │   ├── tests/
│   │   │   │   │   ├── __init__.py
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   └── test_clade_1.py
│   │   │   │   ├── trunk/
│   │   │   │   │   ├── __init__.py
│   │   │   │   │   ├── outer_product_mean.py
│   │   │   │   │   ├── pairformer.py
│   │   │   │   │   ├── recycling.py
│   │   │   │   │   ├── transition.py
│   │   │   │   │   ├── triangle_attention.py
│   │   │   │   │   └── triangle_multiplication.py
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── checkpoint.py
│   │   │   │   ├── compile_plan.py
│   │   │   │   ├── config.py
│   │   │   │   ├── model.py
│   │   │   │   ├── model_card.md
│   │   │   │   ├── model_index.yaml
│   │   │   │   ├── outputs.py
│   │   │   │   ├── parallel_plan.py
│   │   │   │   ├── presets.py
│   │   │   │   └── README.md
│   │   │   ├── common/
│   │   │   │   ├── tests/
│   │   │   │   │   ├── __init__.py
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   └── test_common.py
│   │   │   │   ├── __init__.py
│   │   │   │   ├── alphabets.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── chemistry.py
│   │   │   │   ├── features.py
│   │   │   │   ├── heads.py
│   │   │   │   ├── outputs.py
│   │   │   │   ├── README.md
│   │   │   │   └── tasks.py
│   │   │   └── novafold/
│   │   │       ├── heads/
│   │   │       │   ├── __init__.py
│   │   │       │   ├── affinity.py
│   │   │       │   ├── confidence.py
│   │   │       │   └── interface.py
│   │   │       ├── reference/
│   │   │       │   ├── tiny.py
│   │   │       │   └── tiny.toml
│   │   │       ├── structure/
│   │   │       │   ├── __init__.py
│   │   │       │   ├── atom_attention.py
│   │   │       │   ├── diffusion_module.py
│   │   │       │   ├── noise_schedule.py
│   │   │       │   └── sampler.py
│   │   │       ├── tests/
│   │   │       │   ├── __init__.py
│   │   │       │   ├── BUILD.bazel
│   │   │       │   └── test_novafold.py
│   │   │       ├── trunk/
│   │   │       │   ├── __init__.py
│   │   │       │   ├── outer_product_mean.py
│   │   │       │   ├── pairformer.py
│   │   │       │   ├── recycling.py
│   │   │       │   ├── transition.py
│   │   │       │   ├── triangle_attention.py
│   │   │       │   └── triangle_multiplication.py
│   │   │       ├── __init__.py
│   │   │       ├── BUILD.bazel
│   │   │       ├── checkpoint.py
│   │   │       ├── compile_plan.py
│   │   │       ├── config.py
│   │   │       ├── model.py
│   │   │       ├── model_card.md
│   │   │       ├── model_index.yaml
│   │   │       ├── outputs.py
│   │   │       ├── parallel_plan.py
│   │   │       ├── presets.py
│   │   │       └── README.md
│   │   ├── diffusion/
│   │   │   ├── reference/
│   │   │   │   ├── tiny.py
│   │   │   │   └── tiny.toml
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_diffusion.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── checkpoint.py
│   │   │   ├── compile_plan.py
│   │   │   ├── conditioning.py
│   │   │   ├── config.py
│   │   │   ├── denoiser.py
│   │   │   ├── ema.py
│   │   │   ├── model.py
│   │   │   ├── noise.py
│   │   │   ├── objective.py
│   │   │   ├── outputs.py
│   │   │   ├── parallel_plan.py
│   │   │   ├── preconditioning.py
│   │   │   ├── README.md
│   │   │   ├── rng.py
│   │   │   ├── sampler.py
│   │   │   ├── schedules.py
│   │   │   └── timestep.py
│   │   ├── llm/
│   │   │   ├── reference/
│   │   │   │   ├── tiny.py
│   │   │   │   └── tiny.toml
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_llm.py
│   │   │   ├── __init__.py
│   │   │   ├── attention.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── cache.py
│   │   │   ├── checkpoint.py
│   │   │   ├── compile_plan.py
│   │   │   ├── config.py
│   │   │   ├── embeddings.py
│   │   │   ├── feed_forward.py
│   │   │   ├── generation.py
│   │   │   ├── initialization.py
│   │   │   ├── layer.py
│   │   │   ├── model.py
│   │   │   ├── outputs.py
│   │   │   ├── parallel_plan.py
│   │   │   ├── presets.py
│   │   │   ├── README.md
│   │   │   ├── reward_head.py
│   │   │   ├── rotary.py
│   │   │   └── value_head.py
│   │   ├── moe/
│   │   │   ├── reference/
│   │   │   │   ├── tiny.py
│   │   │   │   └── tiny.toml
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_moe.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── capacity.py
│   │   │   ├── checkpoint.py
│   │   │   ├── compile_plan.py
│   │   │   ├── config.py
│   │   │   ├── dispatch.py
│   │   │   ├── experts.py
│   │   │   ├── layer.py
│   │   │   ├── losses.py
│   │   │   ├── model.py
│   │   │   ├── outputs.py
│   │   │   ├── parallel_plan.py
│   │   │   ├── presets.py
│   │   │   ├── README.md
│   │   │   ├── router.py
│   │   │   └── telemetry.py
│   │   └── multimodal/
│   │       ├── reference/
│   │       │   ├── tiny.py
│   │       │   └── tiny.toml
│   │       ├── tests/
│   │       │   ├── __init__.py
│   │       │   ├── BUILD.bazel
│   │       │   └── test_multimodal.py
│   │       ├── __init__.py
│   │       ├── attention.py
│   │       ├── BUILD.bazel
│   │       ├── checkpoint.py
│   │       ├── compile_plan.py
│   │       ├── config.py
│   │       ├── encoders.py
│   │       ├── fusion.py
│   │       ├── losses.py
│   │       ├── modalities.py
│   │       ├── model.py
│   │       ├── outputs.py
│   │       ├── parallel_plan.py
│   │       ├── projectors.py
│   │       ├── README.md
│   │       └── token_layout.py
│   ├── reference/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_reference.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── README.md
│   │   ├── tiny_biology.py
│   │   ├── tiny_diffusion.py
│   │   ├── tiny_moe.py
│   │   ├── tiny_multimodal.py
│   │   └── tiny_transformer.py
│   ├── registry/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_registry.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── catalog.py
│   │   ├── factory.py
│   │   ├── README.md
│   │   ├── resolver.py
│   │   └── validation.py
│   ├── __init__.py
│   ├── BUILD.bazel
│   ├── factory.py
│   ├── inspection.py
│   ├── py.typed
│   ├── README.md
│   ├── registry.py
│   └── registry.yaml
├── preprocessing/
│   ├── biology/
│   │   ├── entities/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_canonicalize.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── canonicalize.py
│   │   │   ├── deduplicate.py
│   │   │   ├── modifications.py
│   │   │   ├── nucleic_acids.py
│   │   │   ├── proteins.py
│   │   │   └── README.md
│   │   ├── featurization/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_features.py
│   │   │   ├── __init__.py
│   │   │   ├── atoms.py
│   │   │   ├── bonds.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── complexes.py
│   │   │   ├── msa.py
│   │   │   ├── pair.py
│   │   │   ├── README.md
│   │   │   ├── sequence.py
│   │   │   ├── templates.py
│   │   │   └── validation.py
│   │   ├── ligands/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_validation.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── ccd.py
│   │   │   ├── conformers.py
│   │   │   ├── covalent_bonds.py
│   │   │   ├── modifications.py
│   │   │   ├── normalization.py
│   │   │   ├── README.md
│   │   │   └── validation.py
│   │   ├── msa/
│   │   │   ├── adapters/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── jackhmmer.py
│   │   │   │   ├── mmseqs2.py
│   │   │   │   ├── nhmmer.py
│   │   │   │   └── precomputed.py
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── test_pairing.py
│   │   │   │   └── test_search.py
│   │   │   ├── __init__.py
│   │   │   ├── a3m.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── cache.py
│   │   │   ├── crop.py
│   │   │   ├── deduplicate.py
│   │   │   ├── filtering.py
│   │   │   ├── pairing.py
│   │   │   ├── parsing.py
│   │   │   ├── policy.py
│   │   │   ├── README.md
│   │   │   ├── search.py
│   │   │   └── validation.py
│   │   └── templates/
│   │       ├── adapters/
│   │       │   ├── __init__.py
│   │       │   ├── hhsearch.py
│   │       │   ├── hmmsearch.py
│   │       │   └── precomputed.py
│   │       ├── tests/
│   │       │   ├── __init__.py
│   │       │   ├── BUILD.bazel
│   │       │   ├── test_cutoff.py
│   │       │   └── test_selection.py
│   │       ├── __init__.py
│   │       ├── BUILD.bazel
│   │       ├── cache.py
│   │       ├── cutoff.py
│   │       ├── filtering.py
│   │       ├── hits.py
│   │       ├── parsing.py
│   │       ├── README.md
│   │       ├── retrieval.py
│   │       ├── search.py
│   │       ├── selection.py
│   │       └── validation.py
│   ├── cache/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_keys.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── keys.py
│   │   ├── lookup.py
│   │   ├── policy.py
│   │   ├── promotion.py
│   │   ├── README.md
│   │   └── store.py
│   ├── chemistry/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_canonicalize.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── canonicalize.py
│   │   ├── conformers.py
│   │   ├── descriptors.py
│   │   ├── graphs.py
│   │   ├── README.md
│   │   └── validation.py
│   ├── cli/
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── inspect.py
│   │   ├── prepare.py
│   │   ├── search_msa.py
│   │   └── search_templates.py
│   ├── contracts/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_contracts.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── entity.py
│   │   ├── feature_bundle.py
│   │   ├── pipeline.py
│   │   ├── README.md
│   │   ├── search.py
│   │   ├── stage.py
│   │   ├── tool_result.py
│   │   └── validation.py
│   ├── multimodal/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_layout.py
│   │   ├── __init__.py
│   │   ├── alignment.py
│   │   ├── BUILD.bazel
│   │   ├── layout.py
│   │   ├── packing.py
│   │   ├── README.md
│   │   └── validation.py
│   ├── pipeline/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_planner.py
│   │   │   └── test_resume.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── compiler.py
│   │   ├── context.py
│   │   ├── executor.py
│   │   ├── planner.py
│   │   ├── README.md
│   │   ├── resume.py
│   │   ├── stage.py
│   │   └── validation.py
│   ├── provenance/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_manifest.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── database_snapshot.py
│   │   ├── manifest.py
│   │   ├── README.md
│   │   ├── search_record.py
│   │   └── toolchain.py
│   ├── __init__.py
│   ├── BUILD.bazel
│   ├── py.typed
│   └── README.md
├── protocols/
│   ├── compatibility/
│   │   ├── breaking-policy.yaml
│   │   ├── README.md
│   │   └── reserved-fields.yaml
│   ├── events/
│   │   ├── generated/
│   │   │   ├── artifact/
│   │   │   │   └── v1/
│   │   │   │       ├── artifact-events.schema.json
│   │   │   │       ├── event-envelope.schema.json
│   │   │   │       └── README.md
│   │   │   ├── data/
│   │   │   │   └── v1/
│   │   │   │       ├── data-events.schema.json
│   │   │   │       ├── event-envelope.schema.json
│   │   │   │       └── README.md
│   │   │   ├── evaluation/
│   │   │   │   └── v1/
│   │   │   │       ├── evaluation-events.schema.json
│   │   │   │       ├── event-envelope.schema.json
│   │   │   │       └── README.md
│   │   │   ├── model/
│   │   │   │   └── v1/
│   │   │   │       ├── event-envelope.schema.json
│   │   │   │       ├── model-events.schema.json
│   │   │   │       └── README.md
│   │   │   ├── orchestration/
│   │   │   │   └── v1/
│   │   │   │       ├── event-envelope.schema.json
│   │   │   │       ├── orchestration-events.schema.json
│   │   │   │       └── README.md
│   │   │   ├── runtime/
│   │   │   │   └── v1/
│   │   │   │       ├── event-envelope.schema.json
│   │   │   │       ├── README.md
│   │   │   │       └── runtime-events.schema.json
│   │   │   ├── security/
│   │   │   │   └── v1/
│   │   │   │       ├── event-envelope.schema.json
│   │   │   │       ├── README.md
│   │   │   │       └── security-events.schema.json
│   │   │   └── training/
│   │   │       └── v1/
│   │   │           ├── event-envelope.schema.json
│   │   │           ├── README.md
│   │   │           └── training-events.schema.json
│   │   ├── asyncapi.yaml
│   │   ├── BUILD.bazel
│   │   ├── catalog.yaml
│   │   └── README.md
│   ├── mappings/
│   │   ├── BUILD.bazel
│   │   ├── error_codes.yaml
│   │   ├── event_proto.yaml
│   │   ├── identifier_kinds.yaml
│   │   ├── openapi_proto.yaml
│   │   ├── README.md
│   │   └── timestamp_policy.yaml
│   ├── openapi/
│   │   ├── components/
│   │   │   ├── artifacts.yaml
│   │   │   ├── common.yaml
│   │   │   ├── data.yaml
│   │   │   ├── errors.yaml
│   │   │   ├── evaluations.yaml
│   │   │   ├── inference.yaml
│   │   │   ├── models.yaml
│   │   │   ├── pagination.yaml
│   │   │   ├── runs.yaml
│   │   │   ├── security.yaml
│   │   │   └── training.yaml
│   │   ├── admin.openapi.yaml
│   │   ├── BUILD.bazel
│   │   ├── public.openapi.yaml
│   │   └── README.md
│   ├── proto/
│   │   └── mindclade/
│   │       ├── artifact/
│   │       │   └── v1/
│   │       │       ├── artifact.proto
│   │       │       ├── BUILD.bazel
│   │       │       ├── checkpoint.proto
│   │       │       ├── grant.proto
│   │       │       ├── manifest.proto
│   │       │       └── service.proto
│   │       ├── common/
│   │       │   └── v1/
│   │       │       ├── artifact_ref.proto
│   │       │       ├── BUILD.bazel
│   │       │       ├── errors.proto
│   │       │       ├── identifiers.proto
│   │       │       ├── pagination.proto
│   │       │       ├── resources.proto
│   │       │       └── time.proto
│   │       ├── data/
│   │       │   └── v1/
│   │       │       ├── BUILD.bazel
│   │       │       ├── cursor.proto
│   │       │       ├── dataset.proto
│   │       │       ├── record.proto
│   │       │       ├── service.proto
│   │       │       ├── shard.proto
│   │       │       ├── snapshot.proto
│   │       │       └── source.proto
│   │       ├── evaluation/
│   │       │   └── v1/
│   │       │       ├── BUILD.bazel
│   │       │       ├── capability.proto
│   │       │       ├── evaluation.proto
│   │       │       ├── report.proto
│   │       │       ├── safety.proto
│   │       │       └── service.proto
│   │       ├── events/
│   │       │   └── v1/
│   │       │       ├── artifact_events.proto
│   │       │       ├── BUILD.bazel
│   │       │       ├── data_events.proto
│   │       │       ├── envelope.proto
│   │       │       ├── evaluation_events.proto
│   │       │       ├── model_events.proto
│   │       │       ├── orchestration_events.proto
│   │       │       ├── runtime_events.proto
│   │       │       ├── security_events.proto
│   │       │       └── training_events.proto
│   │       ├── inference/
│   │       │   └── v1/
│   │       │       ├── BUILD.bazel
│   │       │       ├── model.proto
│   │       │       ├── request.proto
│   │       │       ├── response.proto
│   │       │       ├── service.proto
│   │       │       └── stream.proto
│   │       ├── orchestration/
│   │       │   └── v1/
│   │       │       ├── BUILD.bazel
│   │       │       ├── job.proto
│   │       │       ├── lease.proto
│   │       │       ├── run.proto
│   │       │       ├── service.proto
│   │       │       ├── stage.proto
│   │       │       └── workflow.proto
│   │       ├── registry/
│   │       │   └── v1/
│   │       │       ├── BUILD.bazel
│   │       │       ├── checkpoint.proto
│   │       │       ├── dataset.proto
│   │       │       ├── deployment.proto
│   │       │       ├── model_bundle.proto
│   │       │       ├── reference_database.proto
│   │       │       ├── release.proto
│   │       │       └── service.proto
│   │       ├── runtime/
│   │       │   └── v1/
│   │       │       ├── artifact_grant.proto
│   │       │       ├── buffer_descriptor.proto
│   │       │       ├── BUILD.bazel
│   │       │       ├── execution_budget.proto
│   │       │       ├── execution_ticket.proto
│   │       │       ├── route_snapshot.proto
│   │       │       ├── service.proto
│   │       │       ├── worker_command.proto
│   │       │       └── worker_status.proto
│   │       ├── security/
│   │       │   └── v1/
│   │       │       ├── attestation.proto
│   │       │       ├── audit.proto
│   │       │       ├── BUILD.bazel
│   │       │       ├── revocation.proto
│   │       │       ├── service.proto
│   │       │       └── weight_access.proto
│   │       └── training/
│   │           └── v1/
│   │               ├── BUILD.bazel
│   │               ├── checkpoint.proto
│   │               ├── config.proto
│   │               ├── job.proto
│   │               ├── progress.proto
│   │               ├── run.proto
│   │               ├── service.proto
│   │               └── topology.proto
│   ├── buf.gen.yaml
│   ├── buf.yaml
│   ├── BUILD.bazel
│   └── README.md
├── research/
│   ├── benchmarks/
│   │   ├── build/
│   │   │   ├── bazel_analysis.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── nix_realization.py
│   │   │   ├── README.md
│   │   │   └── remote_cache.py
│   │   ├── checkpointing/
│   │   │   ├── async_save.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── README.md
│   │   │   └── restore.py
│   │   ├── data/
│   │   │   ├── BUILD.bazel
│   │   │   ├── ingestion.py
│   │   │   ├── packing.py
│   │   │   ├── README.md
│   │   │   └── streaming.py
│   │   ├── distributed/
│   │   │   ├── BUILD.bazel
│   │   │   ├── expert_parallel.py
│   │   │   ├── fsdp2.py
│   │   │   ├── pipeline_parallel.py
│   │   │   ├── README.md
│   │   │   └── tensor_parallel.py
│   │   ├── kernels/
│   │   │   ├── attention.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── diffusion.py
│   │   │   ├── fp8.py
│   │   │   ├── moe.py
│   │   │   ├── README.md
│   │   │   └── tilelang_autotune.py
│   │   ├── models/
│   │   │   ├── biology.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── diffusion.py
│   │   │   ├── llm.py
│   │   │   ├── moe.py
│   │   │   ├── multimodal.py
│   │   │   └── README.md
│   │   └── serving/
│   │       ├── BUILD.bazel
│   │       ├── latency.py
│   │       ├── README.md
│   │       └── throughput.py
│   ├── experiments/
│   │   ├── ablations/
│   │   │   ├── architecture.toml
│   │   │   ├── BUILD.bazel
│   │   │   ├── optimizer.toml
│   │   │   ├── parallelism.toml
│   │   │   ├── precision.toml
│   │   │   ├── README.md
│   │   │   └── tilelang-schedules.toml
│   │   ├── baselines/
│   │   │   ├── BUILD.bazel
│   │   │   ├── README.md
│   │   │   ├── tiny-biology.toml
│   │   │   ├── tiny-diffusion.toml
│   │   │   ├── tiny-moe.toml
│   │   │   └── tiny-transformer.toml
│   │   └── incubator/
│   │       └── example/
│   │           ├── BUILD.bazel
│   │           ├── config.toml
│   │           ├── experiment.py
│   │           ├── hypothesis.md
│   │           └── README.md
│   ├── notebooks/
│   │   ├── 00_environment.ipynb
│   │   ├── 01_dataset_exploration.ipynb
│   │   ├── 02_scaling_laws.ipynb
│   │   ├── 03_training_diagnostics.ipynb
│   │   ├── 04_kernel_analysis.ipynb
│   │   ├── 05_model_evaluation.ipynb
│   │   ├── 06_biomolecular_visualization.ipynb
│   │   ├── BUILD.bazel
│   │   └── README.md
│   ├── BUILD.bazel
│   └── README.md
├── sdk/
│   ├── examples/
│   │   ├── go_inference.go
│   │   ├── python_inference.py
│   │   ├── python_training.py
│   │   ├── README.md
│   │   └── typescript_inference.ts
│   ├── go/
│   │   ├── internal/
│   │   │   └── transport.go
│   │   ├── tests/
│   │   │   ├── client_test.go
│   │   │   └── stream_test.go
│   │   ├── BUILD.bazel
│   │   ├── client.go
│   │   ├── errors.go
│   │   ├── go.mod
│   │   ├── go.sum
│   │   ├── README.md
│   │   ├── stream.go
│   │   └── types.go
│   ├── python/
│   │   ├── mindclade/
│   │   │   ├── __init__.py
│   │   │   ├── artifacts.py
│   │   │   ├── async_client.py
│   │   │   ├── client.py
│   │   │   ├── datasets.py
│   │   │   ├── errors.py
│   │   │   ├── evaluations.py
│   │   │   ├── inference.py
│   │   │   ├── models.py
│   │   │   ├── pagination.py
│   │   │   ├── runs.py
│   │   │   ├── streaming.py
│   │   │   ├── training.py
│   │   │   └── types.py
│   │   ├── tests/
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_client.py
│   │   │   ├── test_streaming.py
│   │   │   └── test_types.py
│   │   ├── BUILD.bazel
│   │   ├── py.typed
│   │   ├── pyproject.toml
│   │   └── README.md
│   ├── typescript/
│   │   ├── src/
│   │   │   ├── artifacts.ts
│   │   │   ├── client.ts
│   │   │   ├── datasets.ts
│   │   │   ├── errors.ts
│   │   │   ├── evaluations.ts
│   │   │   ├── index.ts
│   │   │   ├── inference.ts
│   │   │   ├── models.ts
│   │   │   ├── pagination.ts
│   │   │   ├── runs.ts
│   │   │   └── streaming.ts
│   │   ├── tests/
│   │   │   ├── client.test.ts
│   │   │   └── streaming.test.ts
│   │   ├── BUILD.bazel
│   │   ├── package.json
│   │   ├── README.md
│   │   └── tsconfig.json
│   ├── BUILD.bazel
│   └── README.md
├── services/
│   ├── artifact_proxy/
│   │   ├── src/
│   │   │   ├── access.rs
│   │   │   ├── cache.rs
│   │   │   ├── config.rs
│   │   │   ├── grants.rs
│   │   │   ├── health.rs
│   │   │   ├── main.rs
│   │   │   ├── ranges.rs
│   │   │   ├── server.rs
│   │   │   ├── signing.rs
│   │   │   ├── telemetry.rs
│   │   │   ├── transfer.rs
│   │   │   └── verification.rs
│   │   ├── tests/
│   │   │   ├── integration.rs
│   │   │   └── shutdown.rs
│   │   ├── BUILD.bazel
│   │   ├── Cargo.toml
│   │   ├── PRODUCTION_READINESS.md
│   │   └── README.md
│   ├── control_plane/
│   │   ├── cmd/
│   │   │   ├── api/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── main.go
│   │   │   ├── controller/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── main.go
│   │   │   ├── event_dispatcher/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── main.go
│   │   │   ├── ingestion_controller/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── main.go
│   │   │   ├── scheduler/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── main.go
│   │   │   └── webhook_dispatcher/
│   │   │       ├── BUILD.bazel
│   │   │       └── main.go
│   │   ├── internal/
│   │   │   ├── bootstrap/
│   │   │   │   ├── bootstrap.go
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── components.go
│   │   │   ├── config/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── config.go
│   │   │   │   └── validation.go
│   │   │   ├── store/
│   │   │   │   └── postgres/
│   │   │   │       ├── BUILD.bazel
│   │   │   │       ├── connection.go
│   │   │   │       ├── repositories.go
│   │   │   │       └── transaction.go
│   │   │   ├── transport/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── connect.go
│   │   │   │   ├── grpc.go
│   │   │   │   └── http.go
│   │   │   ├── BUILD.bazel
│   │   │   └── README.md
│   │   ├── migrations/
│   │   │   ├── 000001_core.down.sql
│   │   │   ├── 000001_core.up.sql
│   │   │   ├── 000002_ingestion.down.sql
│   │   │   ├── 000002_ingestion.up.sql
│   │   │   ├── 000003_registry.down.sql
│   │   │   ├── 000003_registry.up.sql
│   │   │   ├── 000004_events.down.sql
│   │   │   ├── 000004_events.up.sql
│   │   │   ├── 000005_runtime_authority.down.sql
│   │   │   └── 000005_runtime_authority.up.sql
│   │   ├── tests/
│   │   │   ├── api_test.go
│   │   │   ├── BUILD.bazel
│   │   │   ├── ingestion_test.go
│   │   │   ├── reconciliation_test.go
│   │   │   └── scheduler_test.go
│   │   ├── BUILD.bazel
│   │   ├── PRODUCTION_READINESS.md
│   │   └── README.md
│   ├── node_agent/
│   │   ├── src/
│   │   │   ├── artifact_transfer.rs
│   │   │   ├── checkpoint_transfer.rs
│   │   │   ├── config.rs
│   │   │   ├── data_stream.rs
│   │   │   ├── diagnostics.rs
│   │   │   ├── health.rs
│   │   │   ├── main.rs
│   │   │   ├── process_supervisor.rs
│   │   │   ├── reference_cache.rs
│   │   │   ├── resource_monitor.rs
│   │   │   ├── server.rs
│   │   │   ├── telemetry.rs
│   │   │   └── tool_runner.rs
│   │   ├── tests/
│   │   │   ├── integration.rs
│   │   │   └── shutdown.rs
│   │   ├── BUILD.bazel
│   │   ├── Cargo.toml
│   │   ├── PRODUCTION_READINESS.md
│   │   └── README.md
│   ├── runtime_gateway/
│   │   ├── src/
│   │   │   ├── admission.rs
│   │   │   ├── auth.rs
│   │   │   ├── config.rs
│   │   │   ├── health.rs
│   │   │   ├── main.rs
│   │   │   ├── routing.rs
│   │   │   ├── server.rs
│   │   │   ├── streaming.rs
│   │   │   ├── telemetry.rs
│   │   │   └── tickets.rs
│   │   ├── tests/
│   │   │   ├── integration.rs
│   │   │   └── shutdown.rs
│   │   ├── BUILD.bazel
│   │   ├── Cargo.toml
│   │   ├── PRODUCTION_READINESS.md
│   │   └── README.md
│   ├── runtime_host/
│   │   ├── src/
│   │   │   ├── config.rs
│   │   │   ├── health.rs
│   │   │   ├── ipc.rs
│   │   │   ├── main.rs
│   │   │   ├── models.rs
│   │   │   ├── processes.rs
│   │   │   ├── resources.rs
│   │   │   ├── server.rs
│   │   │   ├── supervision.rs
│   │   │   └── telemetry.rs
│   │   ├── tests/
│   │   │   ├── integration.rs
│   │   │   └── shutdown.rs
│   │   ├── BUILD.bazel
│   │   ├── Cargo.toml
│   │   ├── PRODUCTION_READINESS.md
│   │   └── README.md
│   ├── workers/
│   │   ├── batch_inference/
│   │   │   ├── tests/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_smoke.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── config.py
│   │   │   ├── executor.py
│   │   │   ├── lifecycle.py
│   │   │   ├── main.py
│   │   │   ├── PRODUCTION_READINESS.md
│   │   │   └── README.md
│   │   ├── curation/
│   │   │   ├── tests/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_smoke.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── config.py
│   │   │   ├── executor.py
│   │   │   ├── lifecycle.py
│   │   │   ├── main.py
│   │   │   ├── PRODUCTION_READINESS.md
│   │   │   └── README.md
│   │   ├── evaluation/
│   │   │   ├── tests/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_smoke.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── config.py
│   │   │   ├── executor.py
│   │   │   ├── lifecycle.py
│   │   │   ├── main.py
│   │   │   ├── PRODUCTION_READINESS.md
│   │   │   └── README.md
│   │   ├── ingestion/
│   │   │   ├── src/
│   │   │   │   ├── config.rs
│   │   │   │   ├── executor.rs
│   │   │   │   ├── lifecycle.rs
│   │   │   │   └── main.rs
│   │   │   ├── tests/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── integration.rs
│   │   │   ├── BUILD.bazel
│   │   │   ├── Cargo.toml
│   │   │   ├── PRODUCTION_READINESS.md
│   │   │   └── README.md
│   │   ├── model_worker/
│   │   │   ├── tests/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_smoke.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── config.py
│   │   │   ├── executor.py
│   │   │   ├── lifecycle.py
│   │   │   ├── main.py
│   │   │   ├── PRODUCTION_READINESS.md
│   │   │   └── README.md
│   │   ├── preprocessing/
│   │   │   ├── tests/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_smoke.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── config.py
│   │   │   ├── executor.py
│   │   │   ├── lifecycle.py
│   │   │   ├── main.py
│   │   │   ├── PRODUCTION_READINESS.md
│   │   │   └── README.md
│   │   ├── reference_builder/
│   │   │   ├── tests/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_smoke.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── config.py
│   │   │   ├── executor.py
│   │   │   ├── lifecycle.py
│   │   │   ├── main.py
│   │   │   ├── PRODUCTION_READINESS.md
│   │   │   └── README.md
│   │   ├── rollout/
│   │   │   ├── tests/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_smoke.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── config.py
│   │   │   ├── executor.py
│   │   │   ├── lifecycle.py
│   │   │   ├── main.py
│   │   │   ├── PRODUCTION_READINESS.md
│   │   │   └── README.md
│   │   ├── simulation/
│   │   │   ├── tests/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_smoke.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── config.py
│   │   │   ├── executor.py
│   │   │   ├── lifecycle.py
│   │   │   ├── main.py
│   │   │   ├── PRODUCTION_READINESS.md
│   │   │   └── README.md
│   │   └── training/
│   │       ├── tests/
│   │       │   ├── BUILD.bazel
│   │       │   └── test_smoke.py
│   │       ├── __init__.py
│   │       ├── BUILD.bazel
│   │       ├── config.py
│   │       ├── executor.py
│   │       ├── lifecycle.py
│   │       ├── main.py
│   │       ├── PRODUCTION_READINESS.md
│   │       └── README.md
│   ├── BUILD.bazel
│   └── README.md
├── serving/
│   ├── batch/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_batching.py
│   │   │   └── test_cancellation.py
│   │   ├── __init__.py
│   │   ├── artifacts.py
│   │   ├── batching.py
│   │   ├── BUILD.bazel
│   │   ├── cancellation.py
│   │   ├── config.py
│   │   ├── executor.py
│   │   ├── health.py
│   │   ├── job.py
│   │   ├── model_loader.py
│   │   ├── queue.py
│   │   ├── README.md
│   │   ├── result.py
│   │   ├── retry.py
│   │   ├── telemetry.py
│   │   └── worker.py
│   ├── contracts/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_contracts.py
│   │   ├── __init__.py
│   │   ├── batch.py
│   │   ├── BUILD.bazel
│   │   ├── model_bundle.py
│   │   ├── README.md
│   │   ├── request.py
│   │   ├── response.py
│   │   ├── runtime_manifest.py
│   │   └── validation.py
│   ├── model_worker/
│   │   ├── batching/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── compatibility.py
│   │   │   ├── continuous.py
│   │   │   ├── planner.py
│   │   │   ├── reservation.py
│   │   │   └── tensor_batch.py
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_batching.py
│   │   │   ├── test_model_loader.py
│   │   │   └── test_training_parity.py
│   │   ├── __init__.py
│   │   ├── biology.py
│   │   ├── BUILD.bazel
│   │   ├── compilation.py
│   │   ├── config.py
│   │   ├── diffusion.py
│   │   ├── generation.py
│   │   ├── health.py
│   │   ├── kv_cache.py
│   │   ├── memory.py
│   │   ├── model_loader.py
│   │   ├── model_runner.py
│   │   ├── multimodal.py
│   │   ├── precision.py
│   │   ├── protocol.py
│   │   ├── README.md
│   │   ├── sampling.py
│   │   ├── shape_buckets.py
│   │   ├── shutdown.py
│   │   ├── telemetry.py
│   │   └── warmup.py
│   ├── rollouts/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_rollouts.py
│   │   ├── __init__.py
│   │   ├── actor.py
│   │   ├── batching.py
│   │   ├── BUILD.bazel
│   │   ├── health.py
│   │   ├── policy_cache.py
│   │   ├── policy_sync.py
│   │   ├── README.md
│   │   ├── sampling.py
│   │   ├── trajectory.py
│   │   └── worker.py
│   ├── runtime/
│   │   ├── src/
│   │   │   ├── admission.rs
│   │   │   ├── batch_envelope.rs
│   │   │   ├── gateway.rs
│   │   │   ├── host.rs
│   │   │   ├── lib.rs
│   │   │   ├── load_shed.rs
│   │   │   ├── request.rs
│   │   │   ├── response.rs
│   │   │   ├── routing.rs
│   │   │   ├── snapshot.rs
│   │   │   ├── streaming.rs
│   │   │   ├── supervision.rs
│   │   │   ├── telemetry.rs
│   │   │   └── ticket.rs
│   │   ├── tests/
│   │   │   ├── gateway.rs
│   │   │   ├── host.rs
│   │   │   └── outage.rs
│   │   ├── BUILD.bazel
│   │   ├── Cargo.toml
│   │   └── README.md
│   ├── safety/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_safety.py
│   │   ├── __init__.py
│   │   ├── audit.py
│   │   ├── BUILD.bazel
│   │   ├── policy.py
│   │   ├── README.md
│   │   ├── screening.py
│   │   └── validation.py
│   ├── testing/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_harness.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── fake_gateway.py
│   │   ├── fake_model.py
│   │   ├── fixtures.py
│   │   ├── goldens.py
│   │   ├── load.py
│   │   └── README.md
│   ├── BUILD.bazel
│   └── README.md
├── tests/
│   ├── e2e/
│   │   ├── BUILD.bazel
│   │   ├── test_batch_inference.py
│   │   ├── test_data_to_training.py
│   │   ├── test_novafold_full_pipeline.py
│   │   ├── test_online_inference.py
│   │   ├── test_release_candidate.py
│   │   └── test_safety_release_gate.py
│   ├── fixtures/
│   │   ├── __init__.py
│   │   ├── checkpoints.py
│   │   ├── configs.py
│   │   ├── data.py
│   │   ├── distributed.py
│   │   ├── models.py
│   │   ├── tiny_dataset.jsonl
│   │   ├── tiny_protein.fasta
│   │   └── tiny_structure.cif
│   ├── goldens/
│   │   ├── digest_vectors.json
│   │   ├── error_codes.json
│   │   ├── execution_tickets.json
│   │   ├── identifier_vectors.json
│   │   ├── manifest_vectors.json
│   │   ├── README.md
│   │   └── tiny_model_metrics.json
│   ├── integration/
│   │   ├── build/
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_bazel_nix_contract.py
│   │   │   ├── test_no_host_tool_leakage.py
│   │   │   └── test_remote_execution_image.py
│   │   ├── control/
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_control_plane.py
│   │   │   ├── test_ingestion_state.py
│   │   │   ├── test_registry_roundtrip.py
│   │   │   └── test_routing_snapshots.py
│   │   ├── cross_language/
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_digest_vectors.py
│   │   │   ├── test_error_codes.py
│   │   │   ├── test_event_envelopes.py
│   │   │   ├── test_execution_tickets.py
│   │   │   ├── test_identifiers.py
│   │   │   ├── test_manifest_roundtrip.py
│   │   │   ├── test_resource_versions.py
│   │   │   └── test_worker_protocol.py
│   │   ├── data/
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_dataset_publication.py
│   │   │   ├── test_ingestion_pipeline.py
│   │   │   └── test_streaming_loader.py
│   │   ├── preprocessing/
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_feature_bundle.py
│   │   │   ├── test_msa_pipeline.py
│   │   │   └── test_template_pipeline.py
│   │   ├── serving/
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_batch_pipeline.py
│   │   │   ├── test_runtime_gateway.py
│   │   │   ├── test_runtime_host.py
│   │   │   └── test_training_serving_parity.py
│   │   └── training/
│   │       ├── BUILD.bazel
│   │       ├── test_train_evaluate.py
│   │       ├── test_train_export.py
│   │       └── test_train_resume.py
│   ├── numerical/
│   │   ├── BUILD.bazel
│   │   ├── test_attention_parity.py
│   │   ├── test_checkpoint_resume_parity.py
│   │   ├── test_diffusion_parity.py
│   │   ├── test_fp8_bf16_parity.py
│   │   ├── test_gradient_parity.py
│   │   ├── test_kernel_provider_parity.py
│   │   ├── test_moe_dispatch_parity.py
│   │   ├── test_rng_determinism.py
│   │   ├── test_tilelang_parity.py
│   │   └── test_world_size_change_parity.py
│   ├── performance/
│   │   ├── baselines.yaml
│   │   ├── BUILD.bazel
│   │   ├── hardware_matrix.yaml
│   │   ├── test_checkpoint_bandwidth.py
│   │   ├── test_data_throughput.py
│   │   ├── test_ingestion_throughput.py
│   │   ├── test_preprocessing_throughput.py
│   │   ├── test_runtime_gateway_latency.py
│   │   ├── test_serving_throughput.py
│   │   ├── test_step_time_budget.py
│   │   └── test_tilelang_regression.py
│   ├── resilience/
│   │   ├── BUILD.bazel
│   │   ├── test_artifact_corruption.py
│   │   ├── test_control_plane_outage.py
│   │   ├── test_node_preemption.py
│   │   ├── test_stale_fencing.py
│   │   └── test_worker_restart.py
│   ├── scale/
│   │   ├── BUILD.bazel
│   │   ├── test_multicluster_scheduling.py
│   │   ├── test_multinode_training.py
│   │   ├── test_preprocessing_fanout.py
│   │   └── test_serving_scale.py
│   ├── security/
│   │   ├── BUILD.bazel
│   │   ├── test_artifact_grants.py
│   │   ├── test_execution_ticket_expiry.py
│   │   ├── test_model_weight_access.py
│   │   ├── test_tenant_isolation.py
│   │   └── test_webhook_signing.py
│   ├── BUILD.bazel
│   ├── conftest.py
│   ├── pytest.ini
│   └── README.md
├── tools/
│   ├── analysis/
│   │   ├── BUILD.bazel
│   │   ├── check_dependency_layers.py
│   │   ├── check_go_layers.py
│   │   ├── check_no_service_imports.py
│   │   ├── check_owners.py
│   │   ├── check_placeholder_packages.py
│   │   ├── graph_dependencies.py
│   │   ├── measure_bazel_analysis.py
│   │   └── README.md
│   ├── build/
│   │   ├── bazel/
│   │   │   ├── bazelrc/
│   │   │   │   ├── ci.bazelrc
│   │   │   │   ├── common.bazelrc
│   │   │   │   ├── cpu.bazelrc
│   │   │   │   ├── cuda.bazelrc
│   │   │   │   ├── debug.bazelrc
│   │   │   │   ├── release.bazelrc
│   │   │   │   ├── remote.bazelrc
│   │   │   │   └── rocm.bazelrc
│   │   │   ├── extensions/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── nix_toolchains.bzl
│   │   │   │   └── toolchain_manifest.bzl
│   │   │   ├── platforms/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── platforms.bzl
│   │   │   ├── rules/
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── container.bzl
│   │   │   │   ├── distributed_test.bzl
│   │   │   │   ├── go.bzl
│   │   │   │   ├── gpu.bzl
│   │   │   │   ├── kernel.bzl
│   │   │   │   ├── python.bzl
│   │   │   │   ├── qualification.bzl
│   │   │   │   ├── release.bzl
│   │   │   │   ├── rust.bzl
│   │   │   │   └── typescript.bzl
│   │   │   ├── toolchains/
│   │   │   │   ├── cc/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── defs.bzl
│   │   │   │   │   └── manifest.bzl
│   │   │   │   ├── cuda/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── defs.bzl
│   │   │   │   │   └── manifest.bzl
│   │   │   │   ├── go/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── defs.bzl
│   │   │   │   │   └── manifest.bzl
│   │   │   │   ├── node/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── defs.bzl
│   │   │   │   │   └── manifest.bzl
│   │   │   │   ├── posix/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── defs.bzl
│   │   │   │   │   └── manifest.bzl
│   │   │   │   ├── protobuf/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── defs.bzl
│   │   │   │   │   └── manifest.bzl
│   │   │   │   ├── python/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── defs.bzl
│   │   │   │   │   └── manifest.bzl
│   │   │   │   ├── rocm/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── defs.bzl
│   │   │   │   │   └── manifest.bzl
│   │   │   │   ├── rust/
│   │   │   │   │   ├── BUILD.bazel
│   │   │   │   │   ├── defs.bzl
│   │   │   │   │   └── manifest.bzl
│   │   │   │   └── tilelang/
│   │   │   │       ├── BUILD.bazel
│   │   │   │       ├── defs.bzl
│   │   │   │       └── manifest.bzl
│   │   │   ├── BUILD.bazel
│   │   │   └── README.md
│   │   └── nix/
│   │       ├── bundles/
│   │       │   ├── cpu.nix
│   │       │   ├── cuda.nix
│   │       │   ├── darwin.nix
│   │       │   ├── default.nix
│   │       │   ├── manifest.nix
│   │       │   └── rocm.nix
│   │       ├── checks/
│   │       │   ├── bazel-version.nix
│   │       │   ├── default.nix
│   │       │   ├── flake-lock.nix
│   │       │   ├── generated-files.nix
│   │       │   ├── no-host-tools.nix
│   │       │   ├── toolchain-manifest.nix
│   │       │   └── version-drift.nix
│   │       ├── images/
│   │       │   ├── cpu.nix
│   │       │   ├── cuda.nix
│   │       │   ├── default.nix
│   │       │   ├── entrypoint.sh
│   │       │   └── rocm.nix
│   │       ├── lib/
│   │       │   ├── default.nix
│   │       │   ├── mk-dev-shell.nix
│   │       │   ├── mk-exec-image.nix
│   │       │   ├── mk-toolchain-bundle.nix
│   │       │   └── platforms.nix
│   │       ├── platforms/
│   │       │   ├── aarch64-darwin.nix
│   │       │   ├── aarch64-linux.nix
│   │       │   ├── default.nix
│   │       │   ├── x86_64-darwin.nix
│   │       │   └── x86_64-linux.nix
│   │       ├── scripts/
│   │       │   ├── bootstrap.sh
│   │       │   ├── export-manifest.py
│   │       │   ├── render-compat-files.py
│   │       │   ├── update-locks.sh
│   │       │   └── verify-manifest.py
│   │       ├── shells/
│   │       │   ├── ci.nix
│   │       │   ├── cpu.nix
│   │       │   ├── cuda.nix
│   │       │   ├── default.nix
│   │       │   ├── docs.nix
│   │       │   ├── release.nix
│   │       │   └── rocm.nix
│   │       ├── toolchains/
│   │       │   ├── bazel.nix
│   │       │   ├── cc.nix
│   │       │   ├── cuda.nix
│   │       │   ├── default.nix
│   │       │   ├── go.nix
│   │       │   ├── node.nix
│   │       │   ├── protobuf.nix
│   │       │   ├── python.nix
│   │       │   ├── rocm.nix
│   │       │   ├── rust.nix
│   │       │   └── tilelang.nix
│   │       ├── BUILD.bazel
│   │       ├── README.md
│   │       └── versions.nix
│   ├── codegen/
│   │   ├── BUILD.bazel
│   │   ├── generate_build_files.py
│   │   ├── generate_config_schema.py
│   │   ├── generate_event_catalog.py
│   │   ├── generate_go_sdk.py
│   │   ├── generate_jsonschema.py
│   │   ├── generate_openapi_clients.py
│   │   ├── generate_proto.sh
│   │   ├── generate_python_sdk.py
│   │   ├── generate_typescript_sdk.py
│   │   ├── README.md
│   │   └── verify_generated.py
│   ├── dev/
│   │   ├── bazelw
│   │   ├── bootstrap.py
│   │   ├── BUILD.bazel
│   │   ├── doctor.py
│   │   ├── format.py
│   │   ├── golden_path.py
│   │   ├── gpu_smoke.py
│   │   ├── graph.py
│   │   ├── lint.py
│   │   ├── nccl_probe.py
│   │   ├── nixw
│   │   ├── README.md
│   │   ├── reproduce_run.py
│   │   ├── seed_local.py
│   │   ├── shell.py
│   │   ├── test.py
│   │   ├── typecheck.py
│   │   └── validate_repository.py
│   ├── qualification/
│   │   ├── build/
│   │   │   ├── BUILD.bazel
│   │   │   ├── check_bazel_hermeticity.py
│   │   │   ├── check_bzlmod_lock.py
│   │   │   ├── check_nix_flake_lock.py
│   │   │   ├── check_remote_execution.py
│   │   │   ├── compare_toolchain_manifests.py
│   │   │   ├── emit_build_evidence.py
│   │   │   ├── README.md
│   │   │   └── verify_execution_image.py
│   │   ├── kernels/
│   │   │   ├── autotune_tilelang.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── inspect_tilelang_ir.py
│   │   │   ├── qualify_tilelang.py
│   │   │   ├── README.md
│   │   │   └── verify_tilelang_manifest.py
│   │   ├── rust/
│   │   │   ├── BUILD.bazel
│   │   │   ├── fuzz.py
│   │   │   ├── miri.py
│   │   │   ├── qualify_parsers.py
│   │   │   ├── qualify_runtime.py
│   │   │   ├── qualify_storage.py
│   │   │   ├── README.md
│   │   │   └── sanitizers.py
│   │   ├── scale/
│   │   │   ├── BUILD.bazel
│   │   │   ├── measure_efficiency.py
│   │   │   ├── README.md
│   │   │   ├── run_scaling_matrix.py
│   │   │   └── verify_recovery.py
│   │   ├── schemas/
│   │   │   ├── build-evidence.schema.json
│   │   │   ├── environment-report.schema.json
│   │   │   ├── numerical-result.schema.json
│   │   │   ├── performance-result.schema.json
│   │   │   ├── release-evidence.schema.json
│   │   │   └── toolchain-manifest.schema.json
│   │   ├── security/
│   │   │   ├── BUILD.bazel
│   │   │   ├── README.md
│   │   │   ├── verify_attestation.py
│   │   │   ├── verify_model_weight_access.py
│   │   │   └── verify_ticket_vectors.py
│   │   ├── BUILD.bazel
│   │   ├── environment.py
│   │   ├── evidence.py
│   │   ├── matrix.py
│   │   ├── numerical.py
│   │   ├── performance.py
│   │   ├── README.md
│   │   ├── recovery.py
│   │   ├── report.py
│   │   ├── reproducibility.py
│   │   ├── run.py
│   │   ├── security.py
│   │   └── verify_release.py
│   ├── release/
│   │   ├── bazel_invocation.py
│   │   ├── BUILD.bazel
│   │   ├── build_evaluation_bundle.py
│   │   ├── build_evidence_bundle.py
│   │   ├── build_model_bundle.py
│   │   ├── build_runtime_bundle.py
│   │   ├── build_safety_bundle.py
│   │   ├── build_toolchain_bundle.py
│   │   ├── build_training_image.py
│   │   ├── changelog.py
│   │   ├── manifest.py
│   │   ├── promote.py
│   │   ├── provenance.py
│   │   ├── publish.py
│   │   ├── README.md
│   │   ├── rollback.py
│   │   ├── sbom.py
│   │   ├── sign.py
│   │   └── verify.py
│   ├── BUILD.bazel
│   └── README.md
├── training/
│   ├── checkpointing/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_reshard.py
│   │   │   └── test_resume.py
│   │   ├── __init__.py
│   │   ├── api.py
│   │   ├── async_save.py
│   │   ├── atomic_commit.py
│   │   ├── backpressure.py
│   │   ├── BUILD.bazel
│   │   ├── conversion.py
│   │   ├── dcp.py
│   │   ├── format.py
│   │   ├── inflight.py
│   │   ├── integrity.py
│   │   ├── lineage.py
│   │   ├── load_planner.py
│   │   ├── manager.py
│   │   ├── manifest.py
│   │   ├── metadata.py
│   │   ├── migration.py
│   │   ├── partial_load.py
│   │   ├── README.md
│   │   ├── request_coalescing.py
│   │   ├── reshard.py
│   │   ├── resume.py
│   │   ├── retention.py
│   │   ├── save_planner.py
│   │   ├── schema.py
│   │   ├── serialization.py
│   │   ├── staging_budget.py
│   │   └── state_registry.py
│   ├── cli/
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── evaluate.py
│   │   ├── inspect.py
│   │   ├── launch.py
│   │   └── train.py
│   ├── contracts/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_contracts.py
│   │   ├── __init__.py
│   │   ├── batch.py
│   │   ├── BUILD.bazel
│   │   ├── checkpoint.py
│   │   ├── effects.py
│   │   ├── engine.py
│   │   ├── evaluation.py
│   │   ├── events.py
│   │   ├── lifecycle.py
│   │   ├── model.py
│   │   ├── optimizer.py
│   │   ├── parallelism.py
│   │   ├── README.md
│   │   ├── result.py
│   │   ├── rollouts.py
│   │   ├── state.py
│   │   ├── step.py
│   │   ├── task.py
│   │   └── trainer.py
│   ├── core/
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_state.py
│   │   │   └── test_trainer.py
│   │   ├── __init__.py
│   │   ├── accumulation.py
│   │   ├── backward.py
│   │   ├── bootstrap.py
│   │   ├── BUILD.bazel
│   │   ├── context.py
│   │   ├── forward.py
│   │   ├── gradient_sync.py
│   │   ├── loop.py
│   │   ├── optimizer_step.py
│   │   ├── progress.py
│   │   ├── README.md
│   │   ├── state.py
│   │   ├── state_registry.py
│   │   ├── step.py
│   │   ├── trainer.py
│   │   └── validation.py
│   ├── distributed/
│   │   ├── backends/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_backends.py
│   │   │   ├── __init__.py
│   │   │   ├── base.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── filesystem.py
│   │   │   ├── memory.py
│   │   │   ├── object_store.py
│   │   │   └── README.md
│   │   ├── debug/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_debug.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── bundle.py
│   │   │   ├── comm_debug_mode.py
│   │   │   ├── flight_recorder.py
│   │   │   ├── monitored_barrier.py
│   │   │   ├── process_dump.py
│   │   │   ├── rank_stacks.py
│   │   │   └── README.md
│   │   ├── moe/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_moe.py
│   │   │   ├── __init__.py
│   │   │   ├── all_to_all.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── dispatch.py
│   │   │   ├── permutation.py
│   │   │   ├── README.md
│   │   │   ├── reductions.py
│   │   │   └── routing_state.py
│   │   ├── parallelism/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_parallelism.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── context_parallel.py
│   │   │   ├── data_parallel.py
│   │   │   ├── ddp.py
│   │   │   ├── expert_parallel.py
│   │   │   ├── fsdp2.py
│   │   │   ├── hybrid.py
│   │   │   ├── pipeline_parallel.py
│   │   │   ├── README.md
│   │   │   ├── replica_group.py
│   │   │   ├── sequence_parallel.py
│   │   │   └── tensor_parallel.py
│   │   ├── pipeline/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_pipeline.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── losses.py
│   │   │   ├── microbatch.py
│   │   │   ├── partitioner.py
│   │   │   ├── README.md
│   │   │   ├── schedules.py
│   │   │   └── stage.py
│   │   ├── plans/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_plans.py
│   │   │   ├── __init__.py
│   │   │   ├── activation_checkpointing.py
│   │   │   ├── base.py
│   │   │   ├── biology.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── compilation.py
│   │   │   ├── diffusion.py
│   │   │   ├── moe.py
│   │   │   ├── multimodal.py
│   │   │   ├── precision.py
│   │   │   ├── README.md
│   │   │   └── transformer.py
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── test_mesh.py
│   │   │   └── test_plan.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── collective_schedule.py
│   │   ├── collectives.py
│   │   ├── comm_hooks.py
│   │   ├── communication.py
│   │   ├── context.py
│   │   ├── execution_scope.py
│   │   ├── gradient_sync.py
│   │   ├── groups.py
│   │   ├── initialize.py
│   │   ├── mesh.py
│   │   ├── metric_reduction.py
│   │   ├── parallel_dims.py
│   │   ├── placements.py
│   │   ├── plan.py
│   │   ├── plan_compiler.py
│   │   ├── plan_fingerprint.py
│   │   ├── plan_validation.py
│   │   ├── ranks.py
│   │   ├── README.md
│   │   ├── reductions.py
│   │   ├── replica_groups.py
│   │   ├── teardown.py
│   │   ├── topology.py
│   │   ├── transformation_order.py
│   │   └── world.py
│   ├── engines/
│   │   ├── fabric/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_fabric.py
│   │   │   ├── __init__.py
│   │   │   ├── adapter.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── engine.py
│   │   │   ├── launcher.py
│   │   │   ├── README.md
│   │   │   ├── strategy.py
│   │   │   └── telemetry.py
│   │   ├── native/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_native.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── engine.py
│   │   │   ├── launcher.py
│   │   │   ├── parallelism.py
│   │   │   ├── precision.py
│   │   │   ├── README.md
│   │   │   └── state.py
│   │   └── torchtitan/
│   │       ├── tests/
│   │       │   ├── __init__.py
│   │       │   ├── BUILD.bazel
│   │       │   └── test_torchtitan.py
│   │       ├── __init__.py
│   │       ├── adapter.py
│   │       ├── BUILD.bazel
│   │       ├── checkpoint.py
│   │       ├── config.py
│   │       ├── engine.py
│   │       ├── parallelism.py
│   │       ├── qualification.py
│   │       ├── README.md
│   │       └── state.py
│   ├── optim/
│   │   ├── algorithms/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_algorithms.py
│   │   │   ├── __init__.py
│   │   │   ├── adafactor.py
│   │   │   ├── adamw.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── distributed_muon.py
│   │   │   ├── lion.py
│   │   │   ├── muon.py
│   │   │   ├── README.md
│   │   │   └── shampoo.py
│   │   ├── gradient/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_gradient.py
│   │   │   ├── __init__.py
│   │   │   ├── accumulation.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── clipping.py
│   │   │   ├── norms.py
│   │   │   ├── overflow.py
│   │   │   ├── README.md
│   │   │   └── transforms.py
│   │   ├── regularization/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_regularization.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── dropout_schedule.py
│   │   │   ├── gradient_noise.py
│   │   │   ├── README.md
│   │   │   └── weight_decay.py
│   │   ├── schedulers/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_schedulers.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── constant.py
│   │   │   ├── cosine.py
│   │   │   ├── inverse_sqrt.py
│   │   │   ├── linear.py
│   │   │   ├── polynomial.py
│   │   │   ├── README.md
│   │   │   └── warmup.py
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_factory.py
│   │   ├── __init__.py
│   │   ├── BUILD.bazel
│   │   ├── context.py
│   │   ├── distributed.py
│   │   ├── factory.py
│   │   ├── fused.py
│   │   ├── parameter_groups.py
│   │   ├── README.md
│   │   ├── state.py
│   │   └── zero1.py
│   ├── runtime/
│   │   ├── callbacks/
│   │   │   ├── integrations/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── artifact_store.py
│   │   │   │   └── control_plane.py
│   │   │   ├── observers/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── exception_report.py
│   │   │   │   ├── learning_rate.py
│   │   │   │   ├── logging.py
│   │   │   │   ├── memory.py
│   │   │   │   ├── metrics.py
│   │   │   │   ├── model_summary.py
│   │   │   │   ├── progress.py
│   │   │   │   └── throughput.py
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_callbacks.py
│   │   │   ├── __init__.py
│   │   │   ├── api.py
│   │   │   ├── backpressure.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── bus.py
│   │   │   ├── coalescing.py
│   │   │   ├── context.py
│   │   │   ├── dead_letter.py
│   │   │   ├── delivery.py
│   │   │   ├── event.py
│   │   │   ├── health.py
│   │   │   ├── idempotency.py
│   │   │   ├── payload.py
│   │   │   ├── queue.py
│   │   │   ├── README.md
│   │   │   ├── retry.py
│   │   │   ├── serialization.py
│   │   │   ├── state.py
│   │   │   ├── subscription.py
│   │   │   ├── validation.py
│   │   │   └── worker.py
│   │   ├── compilation/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_compilation.py
│   │   │   ├── __init__.py
│   │   │   ├── aot_inductor.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── cache.py
│   │   │   ├── compiler.py
│   │   │   ├── cuda_graphs.py
│   │   │   ├── diagnostics.py
│   │   │   ├── dynamic_shapes.py
│   │   │   ├── fallback.py
│   │   │   ├── graph_breaks.py
│   │   │   ├── guards.py
│   │   │   ├── policy.py
│   │   │   ├── README.md
│   │   │   └── regions.py
│   │   ├── hooks/
│   │   │   ├── builtins/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── anomaly_detection.py
│   │   │   │   ├── batch_transform.py
│   │   │   │   ├── finite_check.py
│   │   │   │   ├── gradient_transform.py
│   │   │   │   ├── loss_terms.py
│   │   │   │   └── numerical_guard.py
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_hooks.py
│   │   │   ├── __init__.py
│   │   │   ├── api.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── capabilities.py
│   │   │   ├── chain.py
│   │   │   ├── context.py
│   │   │   ├── directives.py
│   │   │   ├── execution.py
│   │   │   ├── ordering.py
│   │   │   ├── README.md
│   │   │   ├── registry.py
│   │   │   ├── state.py
│   │   │   └── validation.py
│   │   ├── instrumentation/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_instrumentation.py
│   │   │   ├── __init__.py
│   │   │   ├── budget.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── README.md
│   │   │   ├── reduction.py
│   │   │   └── sampling.py
│   │   ├── memory/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_memory.py
│   │   │   ├── __init__.py
│   │   │   ├── activation_checkpointing.py
│   │   │   ├── allocator.py
│   │   │   ├── budget.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── garbage_collection.py
│   │   │   ├── offload.py
│   │   │   ├── README.md
│   │   │   ├── recompute.py
│   │   │   ├── saved_tensors.py
│   │   │   └── snapshot.py
│   │   ├── precision/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_precision.py
│   │   │   ├── __init__.py
│   │   │   ├── autocast.py
│   │   │   ├── bf16.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── dtypes.py
│   │   │   ├── fp16.py
│   │   │   ├── fp8.py
│   │   │   ├── guards.py
│   │   │   ├── loss_scaling.py
│   │   │   ├── numerics.py
│   │   │   ├── policy.py
│   │   │   ├── quantized_training.py
│   │   │   ├── README.md
│   │   │   └── reduction_dtype.py
│   │   ├── resilience/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_resilience.py
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── deadlock.py
│   │   │   ├── failures.py
│   │   │   ├── fault_injection.py
│   │   │   ├── health.py
│   │   │   ├── heartbeat.py
│   │   │   ├── preemption.py
│   │   │   ├── progress.py
│   │   │   ├── README.md
│   │   │   ├── recovery.py
│   │   │   ├── rendezvous.py
│   │   │   ├── retry.py
│   │   │   ├── stragglers.py
│   │   │   └── watchdog.py
│   │   ├── rollouts/
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_rollouts.py
│   │   │   ├── __init__.py
│   │   │   ├── actor.py
│   │   │   ├── backpressure.py
│   │   │   ├── batching.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── coordinator.py
│   │   │   ├── health.py
│   │   │   ├── inference_client.py
│   │   │   ├── policy_version.py
│   │   │   ├── README.md
│   │   │   ├── replay_buffer.py
│   │   │   ├── sampling.py
│   │   │   ├── storage.py
│   │   │   ├── trajectory.py
│   │   │   └── worker.py
│   │   └── telemetry/
│   │       ├── exporters/
│   │       │   ├── __init__.py
│   │       │   ├── jsonl.py
│   │       │   ├── mlflow.py
│   │       │   ├── opentelemetry.py
│   │       │   ├── prometheus.py
│   │       │   ├── tensorboard.py
│   │       │   └── wandb.py
│   │       ├── tests/
│   │       │   ├── __init__.py
│   │       │   ├── BUILD.bazel
│   │       │   └── test_telemetry.py
│   │       ├── __init__.py
│   │       ├── aggregation.py
│   │       ├── BUILD.bazel
│   │       ├── checkpoint.py
│   │       ├── client.py
│   │       ├── communication.py
│   │       ├── events.py
│   │       ├── flops.py
│   │       ├── memory.py
│   │       ├── metric_plan.py
│   │       ├── metrics.py
│   │       ├── README.md
│   │       ├── records.py
│   │       ├── router.py
│   │       ├── tokens.py
│   │       └── traces.py
│   ├── tasks/
│   │   ├── reinforcement/
│   │   │   ├── algorithms/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   ├── grpo.py
│   │   │   │   ├── ppo.py
│   │   │   │   ├── reinforce.py
│   │   │   │   └── vtrace.py
│   │   │   ├── tests/
│   │   │   │   ├── __init__.py
│   │   │   │   ├── BUILD.bazel
│   │   │   │   └── test_reinforcement.py
│   │   │   ├── __init__.py
│   │   │   ├── advantages.py
│   │   │   ├── BUILD.bazel
│   │   │   ├── importance_sampling.py
│   │   │   ├── learner.py
│   │   │   ├── losses.py
│   │   │   ├── metrics.py
│   │   │   ├── off_policy.py
│   │   │   ├── policy_sync.py
│   │   │   ├── README.md
│   │   │   ├── returns.py
│   │   │   ├── state.py
│   │   │   └── task.py
│   │   ├── tests/
│   │   │   ├── __init__.py
│   │   │   ├── BUILD.bazel
│   │   │   └── test_tasks.py
│   │   ├── __init__.py
│   │   ├── biology.py
│   │   ├── BUILD.bazel
│   │   ├── causal_lm.py
│   │   ├── common.py
│   │   ├── contrastive.py
│   │   ├── diffusion.py
│   │   ├── distillation.py
│   │   ├── flow_matching.py
│   │   ├── inverse_folding.py
│   │   ├── masked_modeling.py
│   │   ├── multimodal.py
│   │   ├── multitask.py
│   │   ├── preference_optimization.py
│   │   ├── README.md
│   │   ├── registry.py
│   │   ├── reward_modeling.py
│   │   ├── sequence_to_sequence.py
│   │   ├── structure_prediction.py
│   │   └── supervised.py
│   ├── __init__.py
│   ├── BUILD.bazel
│   ├── py.typed
│   └── README.md
├── .bazelignore
├── .bazelrc
├── .bazelversion
├── .buildifier.json
├── .dockerignore
├── .editorconfig
├── .envrc
├── .gitattributes
├── .gitignore
├── .golangci.yml
├── .pre-commit-config.yaml
├── bazel_downloader.cfg
├── BUILD.bazel
├── Cargo.lock
├── Cargo.toml
├── CHANGELOG.md
├── CODE_OF_CONDUCT.md
├── CONTRIBUTING.md
├── deny.toml
├── flake.lock
├── flake.nix
├── go.mod
├── go.sum
├── GOVERNANCE.md
├── LICENSE
├── MODULE.bazel
├── MODULE.bazel.lock
├── nix.conf
├── NOTICE
├── OWNERS.toml
├── package.json
├── pnpm-lock.yaml
├── pnpm-workspace.yaml
├── pyproject.toml
├── README.md
├── rustfmt.toml
├── SECURITY.md
├── tsconfig.base.json
└── uv.lock
```

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
