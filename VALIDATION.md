# Repository scaffold validation

**Validated:** 2026-08-13  
**Scope:** complete target-state monorepo scaffold, fully implemented reusable Go
foundation, modular control-plane composition path, three runnable Go vertical
slices, architecture/decision/module documentation, and blueprint coverage.

## Materialization statement

The repository deliberately distinguishes implemented code from target-state
boundaries:

- `libs/go/` is source-complete for the reusable Go mechanism layer.
- `control/` contains implemented Go durable-policy/domain boundaries for the
  runtime authority, routing, artifact identity, reference releases, release
  evidence, orchestration, ingestion, and related control concerns.
- `services/control_plane/internal/{bootstrap,config,foundation,transport}`
  implements the canonical Go process composition path.
- `libs/rust/` contains the audited user-supplied Rust foundation plus the
  consolidated runtime/node implementation; deprecated crate names are facades
  over canonical crates rather than parallel implementations.
- `services/runtime_gateway` and `services/runtime_host` contain implemented,
  model-independent Rust cores and are explicitly not production-qualified yet.
- `preprocessing/` contains implemented Python contracts, deterministic DAG,
  cache/provenance primitives, and cross-language fixtures; scientific provider
  leaves remain separately qualified work.
- `examples/go/event_dispatcher` and `examples/go/ingestion_coordinator` are
  runnable, race-tested integrations.
- every explicit path in the approved target-state blueprint is materialized;
  paths restored solely for target-state compatibility are marked as non-authoritative
  scaffolds and point to the consolidated implementation.

A materialized scaffold path is not a production release claim.

## Current inventory

| Metric | Count |
|---|---:|
| Repository files | 4,974 |
| Blueprint paths materialized | 4,475 / 4,475 (100%) |
| Files under `libs/go` | 785 |
| Go source files under `libs/go` | 586 |
| Go test files under `libs/go` | 171 |
| `BUILD.bazel` files under `libs/go` | 92 |
| Package READMEs under `libs/go` | 92 |
| Go package directories under `libs/go` | 89 |
| Files under `libs/rust` | 420 |
| Rust source files under `libs/rust` | 284 |
| Rust test sources under `libs/rust` | 66 |
| Rust crates under `libs/rust` | 30 |
| Root `go.sum` checksum lines | 36 |
| Markdown files under `docs/` | 89 |

The supplied Go archive was used as the base implementation. The expanded
foundation adds strict configuration, signed keyset pagination, resource
versions, detached signing, hardened outbound HTTP, messaging, PostgreSQL
adapters/migrations, `servicekit/production`, and durable cursor, inbox,
leadership, outbox, projector, and work-queue mechanisms.

## Qualification completed in this environment

The following completed successfully:

```text
Go formatting over libs/go, control, control-plane service, and examples       PASS
Go dependency-layer and paved-road checks                                      PASS
Blueprint materialization: 4,475 / 4,475                                      PASS
Normal Go tests over 111 offline-safe package targets                          PASS
Go vet over the same 111 package targets                                       PASS
Race-enabled Go tests over the same 111 package targets, in bounded batches    PASS
Focused production-foundation qualification script                             PASS
Runnable outbox-to-broker event dispatcher                                     PASS
Runnable ingestion leadership/workqueue/cursor/outbox slice                    PASS
Representative control-plane role manifests                                    PASS
```

The 111-package inventory is committed at
`qualification/go/offline-safe-packages.txt`. It covers the complete
standard-library/offline-safe Go foundation, representative control domains,
control-plane process roots, neutral data connector contracts, and the three
integration slices.

## Implemented Go integration behavior

### Event dispatcher

```text
fenced outbox claim
    -> provider-neutral broker publication
    -> message delivery
    -> published transition
    -> readiness/drain/reverse shutdown
```

### Ingestion coordinator

```text
fenced leadership
    -> leased ingestion work
    -> source cursor advancement
    -> outbox append
    -> durable work completion
    -> deterministic drain/shutdown
```

Memory adapters are explicit local/test providers. Production factories must
replace them with qualified PostgreSQL, Pub/Sub, GCS, Redis, and Kubernetes
adapters without changing the foundation state machines.

## Documentation and design alignment completed

`docs/architecture/system-design-reference.md` is the canonical design contract.
`docs/architecture/system-design-traceability.md` maps those decisions to source,
ADRs, tests, and qualification. `tools/analysis/check_code_docs_alignment.py` is
part of the architecture gate and prevents maturity/language/compatibility-facade
claims from drifting away from the code.

The archive includes:

- an architecture decision register and twenty-three detailed ADRs;
- system, language, dependency, control-plane, data, preprocessing, serving,
  training, checkpoint, evaluation, artifact, and release architecture;
- a package-by-package `libs/go` module reference with canonical recipes;
- Go foundation adoption and service golden-path guides;
- service boundaries and production-readiness checklists;
- operational runbooks, security model, and scale/decomposition roadmap;
- the full source blueprint and explicit path manifest.

## Connected qualification still required

This execution environment did not provide the complete connected release
stack. Therefore the artifact does **not** claim:

- full transitive Go module source/`go.sum` closure from the production module mirror (direct requirement checksums are now committed);
- connected provider qualification against real PostgreSQL, Redis, GCS,
  Pub/Sub, Kubernetes, Connect, gRPC, and OpenTelemetry dependencies;
- `bazel test //...`, Bzlmod lock closure, Buildifier, or remote execution;
- `nix flake check` and local/remote toolchain-manifest parity;
- compilation, clippy, fuzz, Miri, sanitizer, performance, or connected-provider
  qualification of the newly implemented Rust runtime foundation and runtime
  service cores;
- broader Python numerical, TileLang, TypeScript, infrastructure, or full product
  implementation/qualification beyond their explicit local evidence.

Required connected closure:

```bash
nix develop .#ci --command go mod tidy
nix develop .#ci --command go test -race -count=1 ./libs/go/... ./control/... \
  ./services/control_plane/... ./examples/go/...
nix develop .#ci --command go vet ./libs/go/... ./control/... \
  ./services/control_plane/... ./examples/go/...
nix develop .#ci --command bazel test //libs/go/... //control/... \
  //services/control_plane/... //examples/go/...
nix flake check
```

Provider, security, performance, fault-injection, image, SBOM, provenance, and
rollback evidence remain release blockers for each promoted deployable.

## Final foundation-hardening validation (2026-08-13)

The final hardening tranche is materialized and the offline foundation-freeze gate passes. This tranche adds and validates:

- affected-only presubmit selection with conservative full-graph fallback for global/toolchain/protocol changes;
- two-level artifact garbage collection: Go owns reachability/lease/pin/hold/retention eligibility, while Rust performs version-conditional byte deletion;
- pinned Rust 1.97.1 release qualification orchestration, committed-lock enforcement, and Cargo/Bazel alignment checks;
- Rust supply-chain policy, runtime compatibility matrix, failure-injection matrix, and explicit performance budgets;
- bounded/redacted node diagnostics and hierarchical resource-budget snapshots;
- one canonical workload envelope shared by ingestion, preprocessing, evaluation, batch, checkpoint/data transfer, rollouts, and simulation;
- four golden vertical release slices: ingestion-to-dataset, preprocessing-to-model-worker, online gateway/host/worker, and training/checkpoint/evidence promotion;
- component-level ownership/SLO/runbook metadata and machine-enforced architecture decisions.

Executed local evidence:

```text
architecture checks                         PASS
affected selector unit tests                3/3 PASS
vertical hardening/contract/reference tests 11/11 PASS
cross-language + worker-runtime tests       12/12 PASS
compatibility matrix                        5 rolling edges PASS
failure-injection matrix                    6 scenarios PASS (policy/offline)
Rust performance policy                     8 budgets PASS (policy; no hardware measurements)
Rust static supply-chain policy             PASS
Rust source-format convention policy        PASS
Rust arithmetic policy                      PASS
artifact/orchestration/ingestion Go tests   PASS
111-package offline Go qualification        PASS including race-enabled foundation/integrations
foundation_freeze.py                         PASS (offline mode)
```

The pinned Rust compiler is not available in this sandbox, and `Cargo.lock` remains an intentionally unresolved connected-lane artifact. Production promotion therefore still requires the repository-owned connected Rust lane to generate and commit the real lock and pass `cargo fmt`, workspace tests, Clippy, docs, cargo-deny, fuzz/Miri, measured performance, and provider-backed failure injection. No local claim substitutes for that evidence.

