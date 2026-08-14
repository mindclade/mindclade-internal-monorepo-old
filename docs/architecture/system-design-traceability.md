# System design traceability

This matrix maps the consolidated system design to repository source,
architecture documentation, accepted decisions, and qualification surfaces. It
exists to prevent architecture from becoming disconnected prose.

| Design concern | Primary source paths | Architecture docs | ADRs / policy | Qualification |
|---|---|---|---|---|
| build graph and toolchains | `MODULE.bazel`, `tools/build/`, `flake.nix`, `tools/` | `build-and-toolchains.md` | ADR-0001, ADR-0002 | build hermeticity, toolchain drift, remote execution |
| language authority | `libs/`, `control/`, `training/`, `serving/`, `kernels/` | `language-boundaries.md`, `system-design-reference.md` | ADR-0003, ADR-0006, ADR-0007, ADR-0008 | dependency budgets, cross-language tests |
| Go mechanism foundation | `libs/go/` | `go-foundation.md` | ADR-0009, ADR-0011, ADR-0012 | `tools/qualification/go/` |
| Go control policy | `control/` | `control-plane.md` | ADR-0015 | domain tests, dependency checks |
| runtime authority | `control/runtime_authority/`, runtime protos, Rust worker protocol | `runtime-authority-and-stage-execution.md` | ADR-0005 | ticket/golden/revocation/fencing tests |
| online routing | `control/routing/`, `services/runtime_gateway/` | `runtime-data-plane.md`, `serving.md` | ADR-0006, ADR-0016 | routing freshness, outage, latency/load-shed tests |
| runtime host/node | `libs/rust/runtime_core/`, `libs/rust/worker_runtime/`, `libs/rust/gpu_host/`, `services/runtime_gateway/`, `services/runtime_host/` | `runtime-data-plane.md` | ADR-0019 | Rust test/clippy/fuzz/resource qualification |
| control vs bulk IPC | `libs/rust/ipc/`, runtime protos | `runtime-authority-and-stage-execution.md` | ADR-0020 | descriptor bounds, IPC corruption/cancellation |
| artifact identity/CAS | `control/artifacts/`, `libs/rust/artifact_cas/`, artifact protos | `artifact-lifecycle.md`, `reference-data-and-release-evidence.md` | ADR-0004, ADR-0022 | digest, range, corruption, GC tests |
| ingestion | `control/ingestion/`, `data/`, ingestion workers | `data-ingestion.md` | ADR-0013, ADR-0020 | source replay, cursor, lineage, bounded parser tests |
| preprocessing | `preprocessing/`, reference registry | `preprocessing.md`, `msa-and-template-search.md` | ADR-0013, ADR-0020, ADR-0022 | cache/provenance/search/feature determinism |
| reference DB releases | registry/reference source, node cache | `reference-data-and-release-evidence.md` | ADR-0022 | snapshot digest/cache corruption qualification |
| model semantics | `models/` | `training.md`, `serving.md` | ADR-0007 | unit/numerical/export/serving parity |
| training state machine | `training/contracts/`, `training/core/`, `training/engines/` | `training.md` | ADR-0007 | trainer/task/state/lifecycle qualification |
| distributed topology | `training/distributed/` | `distributed-training.md` | architecture policy | topology/resume/fault/collective tests |
| checkpointing | `training/checkpointing/`, `libs/rust/checkpoint_io/` | `checkpointing.md` | ADR-0017 | atomic commit, restore, reshard, bandwidth |
| evaluation | `evaluation/` | `evaluation.md` | release/evaluation policy | hidden-set, deterministic metrics, safety gates |
| release evidence graph | `control/registry/releases/`, release protos/tools | `release-evidence.md`, `reference-data-and-release-evidence.md` | ADR-0022 | graph completeness, policy, signature, rollback |
| kernel providers | `kernels/` | `system-design-reference.md` and kernel docs in blueprint | ADR-0008 | numerical/perf/compile qualification |
| configuration resolution | `configs/`, Python config resolver | `system-design-reference.md` | ADR-0023 | deterministic digest/schema/override tests |
| dependency budgets | `architecture/`, `tools/analysis/` | `dependency-rules.md` | ADR-0023 | presubmit budget checks |
| component maturity | `components.toml`, `maturity.toml` | `system-design-reference.md` | ADR-0021 | maturity gate checker |
| service materialization | `services/`, per-service readiness docs | `service-boundaries.md` | ADR-0010, ADR-0018 | readiness, drain, SLO/runbook evidence |
| security boundaries | `docs/security/`, infra security policy | `system-design-reference.md` | execution-ticket/model-weight ADR/policies | security lane, revocation, key-rotation drills |
| operational failure semantics | runbooks + coordination/runtime source | `system-design-reference.md` | derived from accepted ADRs | resilience/fault injection/runbook tests |

## Design-to-code rule

A design statement that controls correctness, security, durability, or resource
safety should eventually have at least one of:

- a type or protocol contract;
- a static architecture/dependency check;
- a unit/conformance test;
- a provider integration test;
- a resilience/performance qualification target;
- a production readiness gate.

Pure prose is acceptable for rationale and future decomposition triggers, but
not as the only enforcement for a safety-critical invariant.

## Automated reconciliation

`tools/dev/validate_repository.py` is the top-level structural validator. It
checks repository hygiene, structured JSON/TOML/YAML parsing when the YAML
parser is available, local Markdown links, and the architecture-policy suite.
`tools/analysis/check_code_docs_alignment.py` specifically verifies that the
canonical language authority, component maturity, runtime service status,
Rust compatibility facades, source traceability, and direct Go module checksum
claims remain synchronized with source.

Use:

```bash
python tools/dev/validate_repository.py
python tools/dev/validate_repository.py --go offline
# connected pinned environment only:
python tools/dev/validate_repository.py --go connected
```
