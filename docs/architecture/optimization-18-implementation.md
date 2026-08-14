# Implementation record: eighteen repository optimizations

**Status:** Accepted implementation plan with executable foundations.  
**Date:** 2026-08-13  
**Scope:** Mindclade internal monorepo

This chapter records the eighteen optimizations applied after the initial
production scaffold. A path existing in the tree is not itself evidence of
implementation. `components.toml` and `maturity.toml` are the machine-readable
status authority; qualification evidence is separate from source completeness.

## Stable language authority

```text
Go        fleet control plane and durable policy
Rust      online/runtime data plane and node execution
Python    scientific, model, inference, evaluation, and training numerics
TileLang  qualified accelerator kernels
TypeScript product surfaces and generated/public clients
```

The runtime boundary is deliberately asymmetric. Go may publish policy used by
an online request but is never a synchronous dependency after valid bounded
authority reaches a Rust runtime. Rust may supervise Python but does not own
model/tensor semantics. Python may request accelerated operations but does not
select an unqualified kernel implementation.

## Implementation matrix

| # | Decision | Executable implementation | Enforcement / evidence | Remaining promotion work |
|---|---|---|---|---|
| 1 | Freeze `libs/go` conceptually | Existing layered foundation retained; `libs/go/ADMISSION.toml` defines the admitted top-level mechanism set | `check_libs_go_admission.py`; Go layer checks | A new generic library requires >=2 production consumers, domain-neutral semantics, conformance tests, docs, owner, and layer review |
| 2 | First-class `control/` domain layer | `control/{admission,artifacts,audit,evaluations,events,ingestion,lineage,metadata,orchestration,registry,routing,runtime_authority,...}` | dependency-layer and budget checks forbid `control -> services/apps` | Continue migrating any reusable Go business policy out of service-private code as vertical slices are implemented |
| 3 | Deepen Rust node/runtime plane without crate explosion | User-supplied Rust foundation is the base; new cohesive `runtime_core`, `bytes_io`, `manifests`, `bounded_parse`, `bio_formats`, `worker_protocol`, `worker_runtime`, `gpu_host`, `python_bridge`, `telemetry`; `common` removed | `check_rust_workspace.py`, dependency budgets, migration policy | Connected Rust compile/clippy/test/fuzz/Miri qualification; migrate remaining compatibility edges over one controlled epoch |
| 4 | One node-wide Rust resource budget | `libs/rust/runtime_core::Budget`, `ResourceVector`, hierarchical RAII reservations; consumed by worker runtime, GPU host, runtime host | runtime-core tests plus Rust workspace check | Real GPU/host measurements and load qualification to calibrate budget estimates |
| 5 | Signed execution/admission/route authority | Runtime protobufs plus Go `control/runtime_authority` and Rust `worker_protocol`; MCCE1 canonical signing bytes, policy/route/revocation epochs, fencing | Go/Python/Rust golden-vector sources; HMAC qualification fixture | Production asymmetric/KMS verifier leaf adapter and key-rotation/compromise drills |
| 6 | Keep Go out of online inference hot path | Go `control/routing` only builds/publishes snapshots; Rust `services/runtime_gateway` verifies grants, selects routes, admits, streams; Rust `runtime_host` revalidates execution authority | route package explicitly contains no online selector; runtime dependency budget | Tokio/Tonic/Tower network leaf, endpoint authentication provider, load/failure qualification |
| 7 | First-class scientific preprocessing | `preprocessing/` contracts, DAG planning, cache keys, provenance, MSA/template/ligand/chemistry/feature boundaries | Python unit tests; ADR-0013 | Full scientific adapters/search algorithms and model-family qualification remain owned by Python domain packages/external hermetic tools |
| 8 | Unified ticketed stage execution | Protobuf `orchestration/v1/stage.proto`; Go `control/orchestration`; Rust `worker_runtime`; Python `libs/python/worker_runtime`; ingestion/preprocessing/evaluation/batch worker adapters | stage validation tests and cross-language protocol tests | Provider/broker composition for each deployable and end-to-end retry/recovery qualification |
| 9 | Separate control IPC from bulk data | `libs/rust/ipc` caps control payloads; runtime `BufferDescriptor`; runtime host validates descriptor count/leases/digests rather than embedding large data | worker-protocol and host source tests | OS-specific shared-memory/fd leaf adapters and measured copy-count budgets |
| 10 | Cross-language conformance | Canonical resource IDs, SHA-256 digests, content-bound resource versions, artifact refs, MCCE1 tickets, event envelope, worker protocol fixtures | `tests/integration/cross_language` in Go/Python and Rust source tests | Execute Rust lane in pinned toolchain; expand goldens with route/revocation/checkpoint/event payload cases |
| 11 | Explicit component maturity | `components.toml` + `maturity.toml` with planned/scaffolded/experimental/implemented/qualified/production/deprecated | `check_component_maturity.py` | Owners advance status only with required test/qualification/SLO/runbook/release evidence |
| 12 | Composable resolved configuration | `libs/python/config` deterministic deep merge, type protection, dotted overrides, source provenance, canonical JSON, SHA-256 `ResolvedConfig` | Python resolver tests | Connect each live recipe/service to schema validation and persist resolved docs in run/release metadata |
| 13 | One root internal Go module | Root `go.mod` owns internal code; independent public SDK is the only admitted nested module | `check_go_modules.py` | None without explicit independently versioned SDK exception |
| 14 | Mechanically enforce Bazel/Nix ownership | repository checker forbids `WORKSPACE`, production Dockerfiles, package installation in actions, host-tool leakage patterns; presubmit wrapper added | `check_build_toolchain_contract.py`, `ci/presubmit/pipeline.py` | Execute Bazel/Nix/remote-execution lanes in connected pinned toolchain environment |
| 15 | Dependency budgets | `architecture/dependency_budgets.toml` limits selected Go/Rust internal edges; checker reads Cargo `[dependencies]` structurally | `check_dependency_budgets.py` | Add budgets when a domain becomes implemented; do not budget scaffold-only symmetry |
| 16 | One immutable artifact identity | Proto `ArtifactRef` separates identity from `ArtifactLocation`; Go `control/artifacts`; Rust `manifests` | artifact tests and primitive golden fixture | Provider-specific replica/retention adapters remain leaf concerns |
| 17 | Reference databases are release artifacts | Proto `registry/v1/reference_database.proto`; Go `control/registry/reference_databases`; preprocessing provenance/cache binds snapshot digests | Go service tests + Python provenance tests | Connected registry/storage publication and real PDB/UniRef/etc. snapshot qualification |
| 18 | Release evidence is a graph | Proto release graph; Go `control/registry/releases` DAG validation/digest/policy/promotion | Go release tests | Connect real training/eval/safety/build/SBOM/provenance evidence producers and promotion workflow |

## What “implemented” means

`implemented` means the repository contains a reviewed contract, substantive
source, tests, build metadata, bounded/failure semantics, and documentation. It
does **not** mean a deployable is production-qualified. In particular, this
execution environment does not contain the pinned Rust/Bazel/Nix toolchain, so
new Rust source is statically validated here but must compile/test in connected
CI before advancing to `qualified`.

## What remains intentionally outside shared foundations

The following remain domain-owned and are forbidden from migrating into broad
`libs/*` catch-alls merely for convenience:

- tenancy, quotas, route/deployment policy, run/job state and release policy;
- MSA pairing, template selection, ligand chemistry, model feature semantics;
- tensor batch construction, CUDA-graph/compile-bucket selection and sampling;
- model architectures, optimizer state, checkpoint semantic registration;
- scientific evaluation policy and release thresholds;
- provider credentials, Kubernetes deployment policy and cloud topology.

## Presubmit

The local/static policy lane is:

```bash
python ci/presubmit/pipeline.py --static-only
```

The connected lane runs the same policy through Bazel and then repository-owned
Bazel tests. CI orchestration selects targets; it does not reimplement build or
qualification logic.
