# Mindclade · Repository scaffold validation

**Baseline validated:** 2026-08-13

**Latest focused validation:** 2026-08-22

**Scope:** complete target-state monorepo scaffold, fully implemented reusable Go
foundation, modular control-plane composition path, three runnable Go vertical
slices, architecture/decision/module documentation, and blueprint coverage.

## Documentation and licensing validation (2026-08-21)

```text
repository-home@2 and common-document@1                    PASS
top-level Markdown links and heading hierarchy             PASS
canonical LICENSE and CODE_OF_CONDUCT digests (7 repos)    PASS
first-party proprietary header coverage                    PASS (320 repaired)
cargo deny check licenses                                  PASS
static presubmit architecture and implementation gates     PASS through 20 gates
dependency budget                                          BLOCKED (pre-existing servicepolicy allowlist drift)
```

The header gate excludes independently licensed agent skills, vendored and
generated files, Next.js machine-owned references, and lockfiles; their own
license and provenance records remain authoritative. The static presubmit
currently stops because `services/control_plane` imports
`go.mindclade.dev/protocols/servicepolicy` outside its dependency allowlist.
This is a source-architecture blocker, not a documentation or license failure,
and no production qualification is inferred from the passing focused checks.

## Materialization statement

The repository deliberately distinguishes implemented code from target-state
boundaries:

- `libs/go/` is source-complete for the reusable Go mechanism layer.
- `control/` contains implemented Go durable-policy/domain boundaries for the
  runtime authority, artifact identity, reference releases, release evidence,
  production-eligibility evidence, release lineage, ingestion, and related
  control concerns. `routing` and `orchestration` are `experimental` in
  `components.toml`, not implemented; ten of `control/orchestration`'s fourteen
  non-test files are still scaffold placeholders. Sixteen further `control/`
  directories — `audit`, `evaluations`, `events`, `metadata`, `registry` (root),
  `registry/datasets`, `registry/deployments`, `runs`, `scheduling`, `tenancy`,
  `usage`, `webhooks`, `weights`, and the ingestion/orchestration/scheduling
  adapter leaves — hold zero non-scaffold production lines and are correctly
  absent from `components.toml`.
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

Recounted 2026-08-19 against the working tree. The previous table was the 2026-08-13 snapshot
and every row in it had drifted; the two rows that had drifted *misleadingly* are called out
below the table, because a reader checking whether this document is current would have taken
either as evidence that it was.

| Metric | Count |
|---|---:|
| Repository files (tracked) | 5,331 |
| Blueprint paths materialized | 4,361 / 4,475 (97.5%) |
| Files under `libs/go` | 767 |
| Go source files under `libs/go` | 573 |
| Go test files under `libs/go` | 169 |
| `BUILD.bazel` files under `libs/go` | 89 |
| Package READMEs under `libs/go` | 89 |
| Go package directories under `libs/go` | 86 |
| Files under `libs/rust` | 407 |
| Rust source files under `libs/rust` | 294 |
| Rust crates under `libs/rust` | 24 |
| Root `go.mod` direct requirements | 18 |
| Root `go.sum` checksum lines | 438 |
| Markdown files under `docs/` | 122 |
| Markdown files, repository-wide | 586 |

Two rows changed meaning rather than magnitude:

- **Blueprint coverage was recorded as 4,475 / 4,475 (100%) and was never true after the Rust
  consolidation.** It is 97.5%, and `tests/integration/test_blueprint_scaffold.py` has measured
  the real number all along — the 100% claim came from `BLUEPRINT_COVERAGE.json`, a root
  snapshot with no generator behind it, which is why that file has been deleted rather than
  refreshed. Those 114 have since been reconciled in `1a3b46c`: the manifest now names the
  layout that shipped, so the two in-flight migrations (`training/distributed`,
  `libs/go/storage/outbox`) and the `.buildkite/` tree this estate does not use are no longer
  counted as unwritten work. Coverage is above 99%, and what remains is genuinely unwritten --
  `.github` templates and metadata, plus paths moving under the live `libs/python`
  restructuring. The per-path breakdown is in that test's baseline comment.
- **`libs/rust` crate count fell from 30 to 24, which is the consolidation succeeding, not
  regression.** The seven retired compatibility crates listed in
  `tools/analysis/check_code_docs_alignment.py` are gone, and that check now fails if any of
  them reappears.

The `libs/go` counts are lower for a different reason: the outbox package moved to
`libs/go/coordination/outbox`, which is where `libs/go/LAYERS.md` places it -- Layer 2, beside
`inbox`, `leadership` and `workqueue`. It imports `retry`, `servicekit` and `storage/lease`, and
a Layer-1 `storage/` contract root may not, so the manifest had been naming a location the
layering forbids. The manifest now agrees with the tree.

Root `go.sum` has gone from 36 lines to 438: the transitive graph is now populated rather than
just the 18 direct requirements. All 18 still carry both their module and `go.mod` checksums,
which is the invariant `check_code_docs_alignment.py` enforces.

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
Bazel loading and language-independent layer graph                              PASS
Bzlmod lock closure and full configured analysis (1,246 top-level targets)      PASS
Bazel //... test suite (351 non-manual tests, pinned macOS/Nix shell)            PASS
MkDocs strict site build (pinned Nix toolchain)                                 PASS
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
