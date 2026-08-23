# Component maturity gap matrix

**Review date:** 2026-08-23
**Scope:** all 94 components declared in `components.toml`, against the six rules
`maturity.toml` enumerates for `production`.
**Decision:** no component advances. 0 of 94 satisfy `production`; the 28 that mechanically
satisfy `qualified` are rejected on document review, not on paperwork.

## Reproducing this

The matrix is computed, not hand-assembled. It is derived from `components.toml`,
`maturity.toml`, `architecture/component_ownership.toml`, `ci/release/targets.yaml`, and the
Bazel `BUILD.bazel` files under each component path:

```sh
tools/dev/nixw develop .#ci --command python3 tools/analysis/check_component_maturity.py --matrix
tools/dev/nixw develop .#ci --command python3 tools/analysis/check_component_maturity.py --matrix --json
```

`Y` means the gate is met. `~` means the evidence document exists somewhere in the tree but
`components.toml` does not reference it, so the gate is *not* met. `.` means no evidence exists.
An unreferenced document does not satisfy a rule; `~` is a lead, not a pass.

## Result

| Gate | Met | Unreferenced evidence |
|---|---|---|
| `requires_tests` | 90 / 94 | 0 |
| `requires_build_target` | 94 / 94 | 0 |
| `requires_qualification` | 28 / 94 | 0 |
| `requires_slo` | 0 / 94 | 20 |
| `requires_runbook` | 0 / 94 | 20 |
| `requires_release_target` | 1 / 94 | 12 |

Blocker sets, which partition all 94 components:

| Components | Missing |
|---|---|
| 61 | qualification, SLO, runbook, release target |
| 28 | SLO, runbook, release target |
| 4 | tests, qualification, SLO, runbook, release target (the four `scaffolded` umbrellas) |
| 1 | qualification, SLO, runbook (`services.go_vanity`, below) |

## Why every component is blocked

**`release_target` is unmet by 93 of 94 and is the binding constraint.** One component declares
the field: `services.go_vanity` names the `go-vanity` catalog entry, which is why its blocker
set is one shorter than everything else's. `ci/release/targets.yaml` is the closed catalog a
release request selects from, and it holds exactly three entries — `go-vanity`,
`protobuf-contracts`, `weights-fixture` — whose Bazel packages overlap 12 declared components.
Naming one is a statement that the catalog entry exists and nothing more:
`ci/release/requests/` contains only its README, so no release request has ever been merged and
nothing in the tree has been released. `ci/release/README.md`
states that production activation additionally requires the reviewed `.github` v5 release,
runner-group policy, capability-specific WIF, connected ARC canary evidence, and a ready GitOps
receiver. None of that is in this repository.

**SLO and runbook evidence exists but is filed in the other record.** `docs/slo/` holds 19
documents and `docs/runbooks/` holds 29, and `architecture/component_ownership.toml` binds an
SLO and a runbook to 20 components — every tier-0 and tier-1 component. `components.toml`, which
is what `maturity.toml`'s rules read, binds neither to any component. The two records must be
wired together before any component can be promoted, and
`check_component_maturity.py` now fails when they disagree.

**Qualification references are counted, not read.** 28 components carry a `qualification` path
and all 28 resolve. Reading them is a different result:

| Reference | Components | What it says |
|---|---|---|
| `protocols/qualification/README.md` | 10 | "**Environment maturity:** Not yet qualified" |
| `QUALIFICATION.md` | 6 | Model lane is "reference and contract evidence only"; closes with "`qualified` and `production` maturity still require connected Linux/Bazel/Nix release evidence" |
| `ci/presubmit/QUALIFICATION.md` | 1 | "Qualification remains pending" |
| `ci/nightly/QUALIFICATION.md` | 1 | "Qualification remains pending" |
| `docs/qualification/ai-gateway-proxy.md` | 1 | "**Candidate date:** 2026-08-21"; Kubernetes package at zero replicas with an invalid image |
| `data/PRODUCTION_READINESS.md` | 1 | "Production promotion: NO-GO pending connected evidence" |
| `infra/security/PRODUCTION_READINESS.md` | 1 | "production activation blocked"; six live gates BLOCKED |
| `infra/terraform/PRODUCTION_READINESS.md` | 1 | "Not ready for production apply"; nine gates MISSING |
| `apps/*/PRODUCTION_READINESS.md` | 2 | Unchecked boxes for drain, tenant isolation, SLOs, provenance |
| `libs/rust/README.md`, `libs/ts/README.md`, `sdk/typescript/README.md` | 3 | Package catalogs and consumer docs. Not qualification records. |
| `docs/qualification/go/control-plane-registry.md` | 1 | "**Qualified:** 2026-08-20" with a connected PostgreSQL evidence list. Real. |

One of the 28 records connected qualification evidence, and it already backs the `qualified`
status of `control.model_registry`. The remaining 27 either state that qualification is pending
or are not qualification documents. Promoting on those would convert a document that says "not
qualified" into a status that says "qualified", which is the exact inversion the maturity model
exists to prevent.

## The SLO documents are not self-certifying either

Counting `docs/slo/` pages would overstate readiness a second way. Five of the nineteen —
`artifact-control.md`, `artifact-proxy.md`, `node-agent.md`, `runtime-gateway.md`,
`runtime-host.md` — assert that "bounded admission, cancellation and shutdown budgets are
release-qualified" and name a 99.9% availability objective. Nothing has been released, none of
those five components carries a qualification reference, and `runtime.artifact_proxy`,
`runtime.gateway`, `runtime.host`, and `runtime.node_agent` are all `implemented`. A document
asserting that a budget is release-qualified is not evidence that it is; that is why this matrix
reports an SLO page as `~` and never as a pass, and why wiring one into `components.toml` should
follow a correction of the page rather than precede it.

## Statuses not advanced, and why

- **28 components mechanically satisfy `qualified`** (tests + qualification + build target):
  `apps.admin`, `apps.console`, `ci.cpu_nightly`, `ci.presubmit`, `control.model_registry`,
  `data`, `infra.security_contracts`, `infra.terraform.modules`, `libs.go`, `libs.rust`,
  `libs.typescript`, the five `models.*` leaves, the ten `protocols.protobuf.*` packages,
  `runtime.ai_gateway_proxy`, and `sdk.typescript`. None advance beyond where they already are:
  their qualification references are the pending statements and READMEs in the table above, and
  `control.model_registry` is the one already recorded `qualified` on the strength of the one
  real record.
- **`services.studio` and `services.go_vanity` are recorded `implemented`, not higher.** Neither
  carries a qualification document, so neither reaches `qualified`; see the section below.
- **`control.routing` stays `experimental`.** It has an SLO and a runbook in the ownership
  registry and no qualification evidence at all.
- **The four `scaffolded` umbrellas** — `models`, `training`, `evaluation`, `kernels` — declare no
  tests, which is correct for reserved space.

## `production_dependency` is enforced

This section previously recorded `production_dependency = false` as read by no code. That is no
longer true. `tools/analysis/check_production_dependencies.py` joins the rule against the real
Go import graph (`tools/analysis/go_import_graph.py`), runs in the static suite, and resolved
the one live violation the join found:

- `control/ingestion`, declared `implemented`, imports `go.mindclade.dev/control/orchestration`
  from non-test production code (`control/ingestion/pipeline.go`, `control/ingestion/stage.go`),
  and `control/orchestration` is `experimental`. The edge carries a dated, ADR-0020-backed entry
  in `PRODUCTION_DEPENDENCY_EXCEPTIONS` expiring 2026-11-14, which the checker rejects if the
  owner is not an OWNERS.toml team, the ADR is not accepted, the date has passed or is more than
  90 days out, or the edge stops being a live violation.

The rule binds every status that does not carry the flag — `implemented`, `qualified`,
`production` and `deprecated` — not only `production`, so it was never vacuous.

One adjacent edge stays outside the join and is recorded here rather than silently omitted:
`services/control_plane/tests/api_test.go` imports `control/routing`, which is `experimental`.
That is test code, not a linked production edge, and `services/control_plane` is still
undeclared, so it is not a production-track source the sweep can anchor on either way.

## Undeclared surface under `services/`

`services/` held **17,721 lines of non-scaffold production Go across 58 packages** with no
`components.toml` entry — the largest remaining instance of the hole a declaration gate exists
to close, since every rule in `maturity.toml` is a rule about a *declared* component. Classified
by file rather than counted, that surface is three deployables:

| Deployable | Packages | Production Go (raw / non-blank) | Test files | Bazel | Declared as | Coverage where tests execute |
|---|---|---|---|---|---|---|
| `services/control_plane` | 45 | 13,732 / 11,006 | 43 | `go_library` + `go_test` throughout | **not declared — blocked** | 24 packages execute tests (`bootstrap` 58.8%, `config` 77.8%, `foundation/orchestration` 85.7%, `providers/api` 62.3%, `providers/apikeys` 84.4%, `store/postgres/admission` 40.8%); the 11 `cmd/*` mains are 9 lines each and 21 packages report 0.0% |
| `services/studio` | 10 | 3,466 / 1,979 | 15 | `go_library` + `go_test` in all 10 | `services.studio`, `implemented`, tier-2 | `authz` 96.7%, `session` 94.2%, `iap` 87.4%, `stream` 77.3%, `metrics` 69.8%, `server` 51.0%, `httpx` 50.6%, `handoff` 35.6%, `cmd/studio` 18.8%; **`runlog` 0.0%** |
| `services/go_vanity` | 3 | 523 / 355 | 3 | `go_library`/`go_binary`/`go_test`, plus `oci_image`/`oci_push` | `services.go_vanity`, `implemented`, tier-2 | `vanity` 93.0%, `service` 91.3%, `cmd/go_vanity` 12.0% |

Two scaffold placeholders sit inside `services/control_plane` and are correctly excluded:
reserved space is not a component. The Rust and Python services under `services/` were already
declared and are not part of this count.

**Granularity is the deployable, not the package.** Go's `internal/` is a compiler-enforced
boundary, so `services/studio/internal/session` has no consumers outside its own deployable for
a status to gate. Declaring 58 components would publish 58 statuses describing two shipping
decisions. `check_component_maturity.py` matches declarations by path prefix precisely so an
owner can declare at the granularity they ship at.

**Two declarations, at the status the evidence supports.** Both are `implemented` — tests and a
building Bazel target, which is what `maturity.toml` requires — and neither is `qualified`,
because neither has a qualification document. `services.studio`'s `tests` list omits
`internal/runlog/runlog_test.go`: it is entirely gated on `STUDIO_TEST_DATABASE_URL` and reports
0.0% statement coverage without it, so listing it would claim evidence CI does not produce.
`internal/handoff` is listed because `contention_test.go` beside the DSN-gated `handoff_test.go`
runs with no database at all. Both are tier-2, matching the record's existing line for browser
surfaces (`apps.console` and `apps.admin`, the TypeScript clients studio serves, are tier-2) and
for a credential-free static responder.

**`services/control_plane` cannot be declared here, and that is the result rather than an
omission.** Its evidence supports `implemented` and its owner is unambiguous — OWNERS.toml
claims `services/control_plane/**` for platform-control explicitly. Its tier is unambiguous too:
it composes `control.runtime_authority`, `control.admission`, `control.artifacts`,
`control.ingestion`, `control.model_registry` and `control.release_evidence`, every one tier-1,
so the deployable hosting them cannot honestly sit below tier-1. That is exactly what blocks it.
`check_component_ownership.py` requires **both** an `slo` and a `runbook` for any tier-0/tier-1
component at `implemented` or above. `docs/runbooks/control-plane-outage.md` exists; `docs/slo/`
has nineteen pages and none of them is the control plane's. The two ways to land the declaration
are to write that SLO — platform-control's to write — or to record the component tier-2, which
would be choosing a criticality to clear a gate rather than to describe the service.

**The gate is widened only as far as it reaches zero.**
`_GO_DECLARATION_GOVERNED_ROOTS` in `check_component_maturity.py` now reads
`("control", "libs", "services/go_vanity", "services/studio")`. The entries are path prefixes,
not top-level directories, so a root can be narrower than a directory. This is an allowlist of
what *is* governed, not a denylist of paths waved through a check that claims to cover them: no
path is exempted by name anywhere inside a governed root, and nothing here asserts
`services/control_plane` is fine. When the control-plane SLO exists, those two entries collapse
to a single `services`, and the test that pins the gap
(`test_services_control_plane_is_a_measured_gap_not_an_assumed_one`) is deleted with them.

## What would change the answer

For any single component, in order: reference its existing `docs/slo/` and `docs/runbooks/`
documents from `components.toml` so both records agree; produce a qualification record that
states what passed rather than what remains pending; add a `ci/release/targets.yaml` entry and
merge a release request against it. The first step is bookkeeping. The second and third require
connected evidence that this repository does not contain.
