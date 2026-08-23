# Component maturity gap matrix

**Review date:** 2026-08-23
**Scope:** all 93 components declared in `components.toml`, against the six rules
`maturity.toml` enumerates for `production`.
**Decision:** no component advances. 0 of 93 satisfy `production`; the 26 that mechanically
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
| `requires_tests` | 89 / 93 | 0 |
| `requires_build_target` | 93 / 93 | 0 |
| `requires_qualification` | 28 / 93 | 0 |
| `requires_slo` | 0 / 93 | 22 |
| `requires_runbook` | 0 / 93 | 22 |
| `requires_release_target` | 0 / 93 | 12 |

Blocker sets, which partition all 93 components:

| Components | Missing |
|---|---|
| 61 | qualification, SLO, runbook, release target |
| 28 | SLO, runbook, release target |
| 4 | tests, qualification, SLO, runbook, release target (the four `scaffolded` umbrellas) |

## Why every component is blocked

**`release_target` is unmet by all 93 and is the binding constraint.** No component declares
the field. `ci/release/targets.yaml` is the closed catalog a release request selects from, and
it holds exactly three entries — `go-vanity`, `protobuf-contracts`, `weights-fixture` — whose
Bazel packages overlap 12 declared components. `ci/release/requests/` contains only its README:
no release request has ever been merged, so nothing in the tree has been released. `ci/release/README.md`
states that production activation additionally requires the reviewed `.github` v5 release,
runner-group policy, capability-specific WIF, connected ARC canary evidence, and a ready GitOps
receiver. None of that is in this repository.

**SLO and runbook evidence exists but is filed in the other record.** `docs/slo/` holds 21
documents and `docs/runbooks/` holds 38, and `architecture/component_ownership.toml` binds an
SLO and a runbook to 22 components — every tier-0 and tier-1 component. `components.toml`, which
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

Counting `docs/slo/` pages would overstate readiness a second way. Five of the twenty-one —
`artifact-control.md`, `artifact-proxy.md`, `node-agent.md`, `runtime-gateway.md`,
`runtime-host.md` — assert that "bounded admission, cancellation and shutdown budgets are
release-qualified" and name a 99.9% availability objective. Nothing has been released, none of
those five components carries a qualification reference, and `runtime.artifact_proxy`,
`runtime.gateway`, `runtime.host`, and `runtime.node_agent` are all `implemented`. A document
asserting that a budget is release-qualified is not evidence that it is; that is why this matrix
reports an SLO page as `~` and never as a pass, and why wiring one into `components.toml` should
follow a correction of the page rather than precede it.

## Statuses not advanced, and why

- **26 components mechanically satisfy `qualified`** (tests + qualification + build target):
  `apps.admin`, `apps.console`, `ci.cpu_nightly`, `ci.presubmit`, `data`,
  `infra.security_contracts`, `infra.terraform.modules`, `libs.rust`, `libs.typescript`, the five
  `models.*` leaves, the ten `protocols.protobuf.*` packages, `runtime.ai_gateway_proxy`, and
  `sdk.typescript`. None advance: their qualification references are the pending statements and
  READMEs in the table above.
- **`control.routing` stays `experimental`.** It has an SLO and a runbook in the ownership
  registry and no qualification evidence at all.
- **The four `scaffolded` umbrellas** — `models`, `training`, `evaluation`, `kernels` — declare no
  tests, which is correct for reserved space.

## The rule this matrix reported as unenforced, and its one breach

`production_dependency = false` on `planned`, `scaffolded`, and `experimental` was read by no
code when this matrix was first assembled: it was vacuously satisfied by its own wording because
no component is `production`, and a passing `check_component_maturity` run was not evidence that
the clause had been checked. `tools/analysis/check_production_dependencies.py` now joins the rule
against `tools/analysis/go_import_graph.py` — the declaration from `components.toml`, the reality
from the import graph — and reports `production dependency check passed`.

The single breach recorded here is closed, and closed at the end that was actually wrong:

- `control/ingestion`, declared `implemented`, still imports
  `go.mindclade.dev/control/orchestration` from non-test production code
  (`control/ingestion/pipeline.go`, `control/ingestion/stage.go`). What changed is the other
  end. `control/orchestration` was invisible to every maturity rule — neither pass nor fail,
  which is worse than a bad status because nothing can report on it — and now carries
  `implemented`, earned on the tests that replaced its `const scaffold_*` files rather than
  asserted to clear the gate. The dated exception the checker held for this edge named that
  resolution and explicitly ruled out "editing a status until this check passes";
  `PRODUCTION_DEPENDENCY_EXCEPTIONS` is empty again, which is its intended steady state.

Two adjacent edges are outside that join and are recorded here rather than silently omitted:
`services/control_plane/tests/api_test.go` imports `control/routing`, which is `experimental`
(test code, not a linked production edge), and `services/control_plane` is itself undeclared, so
it is not a production-track source the sweep can anchor on.

## Undeclared surface

`services/go_vanity` is a deployable Go service with a `BUILD.bazel`, an image target, and the
`go-vanity` entry in the release catalog, but it is not declared in `components.toml` or
`architecture/component_ownership.toml`. It is invisible to the maturity gate. Declaring it
requires an owner and a criticality tier from the owning team.

## What would change the answer

For any single component, in order: reference its existing `docs/slo/` and `docs/runbooks/`
documents from `components.toml` so both records agree; produce a qualification record that
states what passed rather than what remains pending; add a `ci/release/targets.yaml` entry and
merge a release request against it. The first step is bookkeeping. The second and third require
connected evidence that this repository does not contain.
