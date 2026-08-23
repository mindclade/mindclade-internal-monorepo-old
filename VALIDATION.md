# Mindclade · Repository scaffold validation

**Baseline validated:** 2026-08-13

**Latest focused validation:** 2026-08-23

**Scope:** complete target-state monorepo scaffold, fully implemented reusable Go
foundation, modular control-plane composition path, three runnable Go vertical
slices, architecture/decision/module documentation, and blueprint coverage.

## Documentation and licensing validation (2026-08-23)

```text
repository-home@2 and common-document@1                    PASS
top-level Markdown links and heading hierarchy             PASS
canonical LICENSE and CODE_OF_CONDUCT digests (7 repos)    PASS
first-party proprietary header coverage                    PASS
cargo deny check licenses                                  PASS
static presubmit architecture and implementation gates     PASS (29 of 29 gates)
dependency budget                                          PASS
```

The header gate excludes independently licensed agent skills, vendored and
generated files, Next.js machine-owned references, and lockfiles; their own
license and provenance records remain authoritative.

The dependency-budget row previously read `BLOCKED`, on the grounds that
`services/control_plane` imported `go.mindclade.dev/protocols/servicepolicy`
outside its allowlist. That import still exists in `internal/providers/api`, but
the budget now covers it and
`tools/analysis/check_dependency_budgets.py` reports `dependency budget check
passed`. The gate count also moved: the `CHECKS` list in
`tools/analysis/run_architecture_checks.py` now holds 29 entries, not 20. Earlier
revisions of this document recorded 23 and 25; the list grows as gates are
added, and the count above was re-measured from a passing run rather than
carried forward.

No production qualification is inferred from any of these passing checks. They
are repository-only static evidence.

## Control orchestration and scheduling validation (2026-08-23)

`control/orchestration` and `control/scheduling` advanced from reserved package
boundaries to `implemented`. Executed local evidence:

```text
go test -race -count=1 ./control/orchestration                     PASS  2026-08-23
go test -race -count=1 ./control/orchestration/adapters/kubernetes PASS  2026-08-23
go test -race -count=1 ./control/orchestration/adapters/local      PASS  2026-08-23
go test -race -count=1 ./control/orchestration/adapters/slurm      PASS  2026-08-23
go test -race -count=1 ./control/scheduling                        PASS  2026-08-23
pytest tests/integration/cross_language/test_worker_protocol.py    62 passed  2026-08-23
```

Two package sets in that tree are deliberately absent from the rows above,
because a reader counting rows would otherwise read them as covered.
`control/orchestration/launchertest` holds the shared launcher conformance
suite and declares no tests of its own; it is executed three times, once inside
each launcher's own `_test.go`, which is what the three adapter rows above
report. `control/scheduling/adapters/jobset` and
`control/scheduling/adapters/kueue` carry no test files at all — `go test`
prints `[no test files]` and exits 0 for both, which is not a pass.

The cross-language row parses `control/orchestration/state_machine.go` and
`libs/rust/worker_runtime/src/machine.rs` and compares the two attempt
transition tables edge by edge, then pins both against the `WorkerState` enum in
`protocols/proto/mindclade/runtime/v1/worker_status.proto`. Before it existed,
each language asserted its table against a copy of itself, so a transition added
on one side and not the other built clean in both; the first symptom would have
been a stuck run.

All of this is offline, single-host evidence. The Kubernetes launcher was
exercised against the `controller-runtime` fake client, the Slurm launcher
against an in-memory controller fake, and the local launcher against injected
commands plus a re-exec of the test binary. No connected CI run, real cluster,
Kueue or JobSet admission path, PostgreSQL-backed repository, Slurm controller,
or GPU measurement is claimed for either component. `implemented` is not
`qualified`, and neither component carries a qualification reference in
`components.toml`.

ADR-0026 records the three boundaries this work decided — the attempt-state
vocabulary, the `orchestration`/`runs` split, and single-writer ownership of the
Kubernetes objects the blueprint had listed under both packages.

## Materialization statement

The repository deliberately distinguishes implemented code from target-state
boundaries:

- `libs/go/` is source-complete for the reusable Go mechanism layer.
- `control/` contains implemented Go durable-policy/domain boundaries for the
  runtime authority, artifact identity, reference releases, release evidence,
  production-eligibility evidence, release lineage, ingestion, workflow
  orchestration, scheduling policy, and related control concerns. `routing`, `orchestration`, and
  `scheduling` were `experimental`/undeclared in the revision this document previously described;
  all three are now `implemented`. Orchestration and scheduling have their five adapter packages,
  and the scaffold-placeholder count that argued for the lower status is zero — see the 2026-08-23
  section below for the evidence and its limits.
  Thirteen further `control/` directories — `audit`, `evaluations`, `events`,
  `metadata`, `registry` (root), `registry/datasets`, `registry/deployments`,
  `runs`, `tenancy`, `usage`, `webhooks`, `weights`, and the ingestion
  Kubernetes adapter leaf — hold zero non-scaffold production lines and are
  correctly absent from `components.toml`.
- `services/control_plane/internal/{bootstrap,config,foundation,transport}`
  implements the canonical Go process composition path.
- `libs/rust/` contains the audited user-supplied Rust foundation plus the
  consolidated runtime/node implementation, in 25 workspace crates. The seven
  crates retired in the 2026-08 consolidation — `clock`, `retry`,
  `resource_version`, `observability`, `artifact_manifest`, `byte_spec`, and
  `python_bindings` — were **removed**, not left as facades: they are gone from
  `libs/rust/`, from the workspace members, and from `Cargo.lock`, and
  `tools/analysis/check_code_docs_alignment.py` fails if any of them reappears.
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

## Affected Bazel and CPU-nightly implementation (2026-08-22)

The presubmit implementation now uses Bazel's post-loading reverse-dependency
graph, separate analysis/test target files, rename-aware Git input, fail-closed
query behavior, versioned BEP/profile/selection evidence, and a stable
`bazel / verdict` context. Pull requests, merge groups, main pushes, and the CPU
nightly currently retain full `//...` execution. The affected selector remains
available for explicit local qualification but is not active on protected events.

Repository-wide fallback inputs now live in one strict, versioned contract.
Static presubmit inventories every root entry and every `tools/` authority, so a
new graph-control surface cannot silently bypass full validation. A
checksum-pinned target-determinator candidate remains activation-blocked by the
unqualified remote cache, absent externally pinned required workflow, incomplete
Linux full-graph evidence, checkout restoration behavior under interruption, and
Bazel 9 version-string fallback.
Artifact-plan Phase 5 is explicitly incomplete; this source does not activate or
claim graph-native selection. Immutable fallback anchors and event-policy tests
preserve full pull-request, merge-group, protected-main, and nightly correctness
gates until connected affected-mode activation is separately reviewed.
Protected execution also rejects redirected Git files, invalid linked worktrees,
symlinked checkout roots,
YAML aliases/tags/duplicate keys, and block-scalar normalization drift. The exact
validated disk or loopback-remote cache role is encoded only in generated
`user.bazelrc`; tracked `.bazelrc` is rejected if it can override that authority.

Connected pull-request, merge-group, scheduled-run, required-check, and 28-day
latency evidence is pending, as is activation of the pinned organization required
workflow outside the pull request's mutable trust boundary. Local aarch64-Darwin
real-graph validation passed with two analysis targets and one test. An initial
external Go-proxy TLS timeout failed closed as `AFFECTED-SELECT-007`; retrying after
repository materialization passed and never produced an empty affected set.

## Rust hardening validation (2026-08-20)

The pinned Rust 1.97.1 toolchain, committed lockfile, and `cargo-deny` are
available in this workspace. The canonical presubmit passes with all workspace
features and targets enabled:

```text
cargo fmt --all -- --check                                      PASS
cargo test --workspace --all-targets --all-features --locked    PASS
cargo clippy ... -- -D warnings                                 PASS
cargo test --workspace --doc / cargo doc                        PASS
Rust format/arithmetic/implementation/Cargo-Bazel static checks PASS
cargo-deny advisories/bans/licenses/sources                     PASS
compatibility matrix and 6 executed failure-injection scenarios PASS
Rust performance policy (8 budgets; 2 portable measurements)    PASS
```

This hardening pass also replaced placeholder Rust tests with behavioral cases
and added regression coverage for bounded verified reads, local-store
conditions and range integrity, gateway and host drain admission, early
policy-snapshot rejection, IPC permissions/sealing, tool output/timeouts,
reference-path escape, socket-path preservation, and dead-process cleanup.
Connected Linux unsafe qualification, Miri/fuzz/sanitizers, provider-backed
failure injection, the six hardware/provider performance budgets not covered by
the portable probe, Bazel/Nix release builds, and deployment evidence remain
promotion blockers.

## Current inventory

Recounted 2026-08-23 against the working tree. The previous table was the 2026-08-13 snapshot
and every row in it had drifted; the two rows that had drifted *misleadingly* are called out
below the table, because a reader checking whether this document is current would have taken
either as evidence that it was.

| Metric | Count |
|---|---:|
| Repository files (tracked) | 6,285 |
| Blueprint paths materialized | 4,494 / 4,494 (100.0%) |
| Files under `libs/go` | 768 |
| Go source files under `libs/go` (incl. tests) | 574 |
| Go test files under `libs/go` | 170 |
| `BUILD.bazel` files under `libs/go` | 89 |
| Package READMEs under `libs/go` | 89 |
| Go package directories under `libs/go` | 86 |
| Files under `libs/rust` | 420 |
| Rust source files under `libs/rust` | 304 |
| Rust crates under `libs/rust` | 25 |
| Root `go.mod` direct requirements | 21 |
| Root `go.mod` indirect requirements | 76 |
| Root `go.sum` checksum lines | 286 |
| Root `go.sum` distinct modules | 134 |
| Markdown files under `docs/` | 141 |
| Markdown files, repository-wide | 696 |

Rows worth a note, because earlier revisions of this table were wrong about them:

- **Blueprint coverage is 100.0%: 4,494 of 4,494 manifest paths materialized, zero missing.**
  `tools/analysis/check_blueprint_scaffold.py` reports `coverage_percent 100.0` and
  `missing_paths 0`, and `MATERIALIZATION_BASELINE` in
  `tests/integration/test_blueprint_scaffold.py` is asserted `== 0`. Both halves of the older
  `4,361 / 4,475 (97.5%)` figure have moved — the manifest
  `docs/blueprint/production-monorepo-paths.txt` now carries 4,494 path lines, and nothing in
  it is unwritten. The revision before that recorded `4,475 / 4,475` from
  `BLUEPRINT_COVERAGE.json`, a root snapshot with no generator behind it, since deleted. The
  number here is now read from the checker rather than from a snapshot.
- **`libs/rust` holds 25 crates.** The seven crates retired in the 2026-08 consolidation are
  listed in `tools/analysis/check_code_docs_alignment.py`, they are gone from `libs/rust/`, the
  workspace members, and `Cargo.lock`, and that check fails if any of them reappears. An
  earlier revision of this table said 24; the count moves as crates are added, and it is not a
  consolidation regression.

The outbox package lives at `libs/go/coordination/outbox`, which is where `libs/go/LAYERS.md`
places it -- Layer 2, beside `inbox`, `leadership` and `workqueue`. It imports `retry`,
`servicekit` and `storage/lease`, and a Layer-1 `storage/` contract root may not, so an earlier
manifest had been naming a location the layering forbids. The manifest agrees with the tree.

Root `go.mod` declares 21 direct requirements and 76 indirect ones; `go.sum` carries 286 lines
covering 134 distinct modules. Every direct requirement carries both its module and its
`go.mod` checksum, which is the invariant `check_code_docs_alignment.py` enforces, and that gate
passes. No gate asserts the line count itself: it moves with every dependency change, and a gate
on it would teach people to edit the number rather than read the diff. Earlier revisions of this
document recorded 18 direct requirements and 438 `go.sum` lines; both have since changed.

The supplied Go archive was used as the base implementation. The expanded
foundation adds strict configuration, signed keyset pagination, resource
versions, detached signing, hardened outbound HTTP, messaging, PostgreSQL
adapters/migrations, `servicekit/production`, and durable cursor, inbox,
leadership, outbox, projector, and work-queue mechanisms.

## Qualification completed in this environment

Rows carrying a date were re-measured on that date. The rest were executed in the
earlier pass this document records and have not been re-run since -- they are
dated evidence, not a statement about the tree as it stands today.

```text
Go formatting over libs/go, control, control-plane service, and examples       PASS
Go dependency-layer and paved-road checks                                      PASS
Blueprint materialization: 4,494 / 4,494 (100.0%)                              PASS  2026-08-23
Static presubmit architecture gates: 29 of 29                                  PASS  2026-08-23
Normal Go tests over 111 offline-safe package targets                          PASS
Go vet over the same 111 package targets                                       PASS
Race-enabled Go tests over the same 111 package targets, in bounded batches    PASS
Focused production-foundation qualification script                             PASS
Runnable outbox-to-broker event dispatcher                                     PASS
Runnable ingestion leadership/workqueue/cursor/outbox slice                    PASS
Representative control-plane role manifests                                    PASS
Bazel loading and language-independent layer graph                             PASS
Bzlmod lock closure and full configured analysis                               PASS
MkDocs strict site build (pinned Nix toolchain)                                PASS  2026-08-23
```

The Bazel graph resolves 2,334 rule targets under `//...`, of which 439 are
non-manual test targets (`bazelw query`, 2026-08-23). Earlier revisions of this
document recorded 1,246 top-level targets and a passing `//...` run over 351
non-manual tests. That execution has **not** been repeated at the current target
count, so no passing run over all 439 is claimed here; the two numbers above are
graph measurements, not test results.

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

- an architecture decision register and twenty-five detailed ADRs;
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
- connected Linux and remote-execution parity for the locally passing Bazel
  graph, Bzlmod lock, and test suite;
- `nix flake check` and local/remote toolchain-manifest parity;
- fuzz, Miri, sanitizer, complete hardware/provider performance, or connected-provider
  qualification of the Rust runtime foundation and runtime service cores
  (the canonical presubmit, including cargo-deny and portable probes, now passes);
- broader Python numerical, TileLang, TypeScript, infrastructure, or full product
  implementation/qualification beyond their explicit local evidence.

Required connected closure:

```bash
tools/dev/nixw develop .#ci --command go mod tidy
tools/dev/nixw develop .#ci --command go test -race -count=1 ./libs/go/... ./control/... \
  ./services/control_plane/... ./examples/go/...
tools/dev/nixw develop .#ci --command go vet ./libs/go/... ./control/... \
  ./services/control_plane/... ./examples/go/...
tools/dev/nixw develop .#ci --command tools/dev/bazelw test //... --config=ci
tools/dev/nixw flake check
```

Provider, security, performance, fault-injection, image, SBOM, provenance, and
rollback evidence remain release blockers for each promoted deployable.

## Final foundation-hardening validation (2026-08-13)

The final hardening tranche is materialized and the offline foundation-freeze gate passes. This tranche adds and validates:

- Bazel-authoritative affected-selection qualification source, kept dormant behind full protected-event validation until its connected activation gates pass;
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
affected/presubmit/nightly unit tests       32/32 PASS (host Python)
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

The 2026-08-20 pass supersedes the historical toolchain limitation above: the
pinned compiler and committed `Cargo.lock` are now present, and the canonical
Rust presubmit passes. Production promotion still requires Bazel/Nix release
qualification, fuzz/Miri/sanitizers, the remaining hardware/provider performance
measurements, provider-backed failure injection, image/provenance checks, and
deployment rollback. No local claim substitutes for that evidence.
