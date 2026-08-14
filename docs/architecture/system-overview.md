# System overview

## Mission and scope

The monorepo supports a frontier biological-model platform spanning external
data ingestion, scientific curation, model preprocessing, training,
evaluation, batch and online inference, artifacts, releases, and product
surfaces. It must scale from a small team to multi-cluster operation without
forcing early microservice fragmentation.

## Governing principles

1. Bazel owns the build and release graph; Nix owns pinned toolchains.
2. Contracts and reusable mechanisms are separated from deployable processes.
3. Go owns fleet policy and durable control state.
4. Rust owns latency-sensitive networking, byte movement, and node execution.
5. Python/PyTorch owns scientific and numerical semantics.
6. TileLang kernels are optional providers behind fail-closed qualification.
7. Artifacts, datasets, checkpoints, route snapshots, and database snapshots
   are immutable and digest-addressed.
8. Long-running work is durable, fenced, retryable, observable, and resumable.
9. Services are composition roots, not reusable libraries.
10. A scaffold path is not evidence of production completion.

## Logical planes

```text
Users, SDKs, apps
        |
        v
Go control plane
  tenancy | runs | registry | scheduling | ingestion | audit | usage
        |
        +---- signed grants, execution tickets, route snapshots --------+
        |                                                               |
        v                                                               v
Durable workflows                                                Rust runtime plane
  ingestion, preprocessing,                                      gateway, host, node agent,
  training, evaluation, batch                                    artifacts, local admission
        |                                                               |
        +---------------------- Python/PyTorch --------------------------+
                               scientific engines,
                               models, trainer, evaluation
                                      |
                                 qualified TileLang
```

## Primary end-to-end flows

### External data to published dataset

```text
source snapshot discovery (Go)
  -> bounded fetch/parse and byte transfer (Rust)
  -> raw artifact commit (Rust artifact plane)
  -> canonicalization and curation (Python)
  -> quality, license, lineage, and safety gates (Python + Go policy)
  -> deterministic model-ready shards (Python/Rust streaming)
  -> immutable dataset version and publication event (Go)
```

### Full NovaFold-style prediction

```text
run submission (Go)
  -> durable preprocessing DAG (Go)
  -> MSA/template/reference work (Python semantics + Rust node execution)
  -> immutable PreprocessedInputBundle
  -> Python/PyTorch GPU inference
  -> confidence/ranking/evaluation
  -> artifacts and terminal run state
```

### Online inference

```text
Go publishes route snapshot and bounded admission grant
  -> Rust validates locally, admits, streams, and supervises
  -> Python performs final tensor batching and model execution
  -> Rust multiplexes the response and accounts local resources
```

The Go control plane is not a synchronous dependency after admission.

### Training to release

```text
resolved configuration
  -> Go quota/policy admission and Kubernetes workload creation
  -> Rust node staging and transfer
  -> Python trainer and distributed execution
  -> atomic distributed checkpoints
  -> independent evaluation and safety suites
  -> evidence bundle
  -> Go registry promotion and route publication
```

## Repository ownership map

| Path | Owns |
|---|---|
| `protocols/` | Canonical RPC, event, public API, runtime-ticket, and manifest contracts |
| `libs/` | Stable cross-domain mechanisms by language |
| `control/` | Reusable Go domain policy and durable state machines |
| `data/` | Data contracts, connectors, ingestion semantics, curation, datasets, loaders |
| `preprocessing/` | Scientific preparation, MSA/template/ligand/feature semantics |
| `models/` | Model contracts, components, families, adapters, registries |
| `training/` | Trainer contracts, engines, distributed runtime, optimizers, checkpoint orchestration |
| `evaluation/` | Independent evaluation harnesses, suites, metrics, safety, reporting |
| `serving/` | Reusable model-worker and inference implementation libraries |
| `services/` | Deployable composition roots only |
| `apps/` | Browser product surfaces consuming SDKs/contracts |
| `infra/` | Terraform, Kubernetes, GitOps, security, observability |
| `tools/` and `ci/` | Build, code generation, qualification, release, developer workflows |

## Operational invariants

- every queue, pool, parser, request, and shutdown path is bounded;
- stale fencing tokens cannot commit state or artifacts;
- database mutation, audit, and event append share one transaction;
- event consumers use inbox/idempotency and monotonic cursors;
- services become unready before drain and stop in reverse dependency order;
- configuration resolves to one immutable, validated, redacted, digestible
  snapshot;
- cross-language representations are generated or verified against golden
  vectors;
- release promotion requires build, numerical, performance, safety, security,
  and rollback evidence.

## Current implementation status

This overview is intentionally concise. The canonical implementation/status
reference is `system-design-reference.md`. Current substantive implementations
include the Go foundation and control-plane seams, durable coordination, the
adopted/deepened Rust runtime and node foundations plus gateway/host cores,
runtime authority/routing/artifact/reference/evidence contracts, deterministic
Python configuration resolution, and preprocessing DAG/cache/provenance seams.
Provider-backed leaves, full scientific/model implementations, scale validation,
and production cloud qualification remain governed by component maturity and
qualification evidence.
