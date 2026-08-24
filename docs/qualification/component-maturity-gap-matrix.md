# Component maturity gap matrix

**Review date:** 2026-08-23
**Scope:** all 106 components declared in `components.toml`, against the six rules
`maturity.toml` enumerates for `production`.
**Decision:** no component is production-ready. 0 of 106 satisfy `production`; the 30 that mechanically
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
| `requires_tests` | 102 / 106 | 0 |
| `requires_build_target` | 106 / 106 | 0 |
| `requires_qualification` | 30 / 106 | 0 |
| `requires_slo` | 33 / 106 | 0 |
| `requires_runbook` | 37 / 106 | 0 |
| `requires_release_target` | 11 / 106 | 2 |

Blocker sets, which partition all 106 components:

| Components | Missing |
|---|---|
| 44 | qualification, SLO, runbook, release target |
| 25 | qualification, release target |
| 10 | SLO, runbook |
| 10 | SLO, runbook, release target |
| 8 | release target |
| 4 | tests, qualification, SLO, runbook, release target (the four `scaffolded` umbrellas) |
| 2 | qualification, SLO, release target |
| 2 | SLO, release target |
| 1 | qualification, SLO, runbook |

## Why every component is blocked

**`release_target` is unmet by 95 of 106 and remains a binding constraint.** Eleven components
declare a catalog entry: `services.go_vanity` names `go-vanity`, and the ten protobuf components
name `protobuf-contracts`. `ci/release/targets.yaml` is the closed catalog a release request
selects from; it also contains `weights-fixture`, whose Bazel packages give two more components
unreferenced candidate evidence. Naming a catalog entry is a statement that it exists and
nothing more:
`ci/release/requests/` contains only its README, so no release request has ever been merged and
nothing in the tree has been released. `ci/release/README.md`
states that production activation additionally requires the reviewed `.github` v5 release,
runner-group policy, capability-specific WIF, connected ARC canary evidence, and a ready GitOps
receiver. None of that is in this repository.

**SLO and runbook evidence is wired where an owner has made the claim.** `docs/slo/` holds 22
documents and `docs/runbooks/` holds 38. `components.toml` and
`architecture/component_ownership.toml` agree on the 33 SLO references and 37 runbook
references the maturity matrix counts; `check_component_maturity.py` fails when mirrored fields
disagree. Components without a referenced document continue to fail the corresponding gate.

**Qualification references are counted, not read.** 30 components carry a `qualification` path
and all 30 resolve. Reading them is a different result:

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
| `docs/qualification/control-artifacts.md` | 1 | A repository-local contract and test inventory; connected production qualification remains explicitly out of scope. |
| `docs/qualification/go/control-plane-registry.md` | 2 | "**Qualified:** 2026-08-20" with a connected PostgreSQL evidence list. Real. |

Two of the 30 component references share connected registry evidence, backing the `qualified`
statuses of `control.model_registry` and `services.control_plane.registry`. The remaining 28
either state that qualification is pending or are not qualification documents. Advancing those
further on document presence alone would convert a document that says "not
qualified" into a status that says "qualified", which is the exact inversion the maturity model
exists to prevent.

## The SLO documents are not self-certifying either

Counting `docs/slo/` pages would overstate readiness a second way. Five of the twenty-two —
`artifact-control.md`, `artifact-proxy.md`, `node-agent.md`, `runtime-gateway.md`,
`runtime-host.md` — assert that "bounded admission, cancellation and shutdown budgets are
release-qualified" and name a 99.9% availability objective. Nothing has been released, none of
those five components carries a qualification reference, and `runtime.artifact_proxy`,
`runtime.gateway`, `runtime.host`, and `runtime.node_agent` are all `implemented`. A document
asserting that a budget is release-qualified is not evidence that it is. The matrix reports a
referenced SLO as mechanically present, not as connected qualification evidence; the production
decision above therefore remains blocked even where the SLO gate is met.

## Status dispositions, and why

- **30 components mechanically satisfy `qualified`** (tests + qualification + build target):
  `apps.admin`, `apps.console`, `ci.cpu_nightly`, `ci.presubmit`, `control.artifacts`, `control.model_registry`,
  `data`, `infra.security_contracts`, `infra.terraform.modules`, `libs.go`, `libs.rust`,
  `libs.typescript`, the five `models.*` leaves, the ten `protocols.protobuf.*` packages,
  `runtime.ai_gateway_proxy`, `sdk.typescript`, and `services.control_plane.registry`. None
  advance beyond where they already are:
  their qualification references are the pending statements and READMEs in the table above, and
  `control.model_registry` and `services.control_plane.registry` share the connected registry
  record.
- **`services.studio` and `services.go_vanity` are recorded `implemented`, not higher.** Neither
  carries a qualification document, so neither reaches `qualified`; see the section below.
- **`control.routing` advances only to `implemented`.** Its package tests now cover immutable
  signed snapshots and exact retry after publication failure. It has an SLO and a runbook but no
  durable repository, connected publisher, or qualification evidence, so it cannot advance to
  `qualified`.
- **The four `scaffolded` umbrellas** — `models`, `training`, `evaluation`, `kernels` — declare no
  tests, which is correct for reserved space.

## `production_dependency` is enforced

This section previously recorded `production_dependency = false` as read by no code. That is no
longer true. `tools/analysis/check_production_dependencies.py` joins the rule against the real
Go import graph (`tools/analysis/go_import_graph.py`) and runs in the static suite.

The one previously recorded breach is closed at the end that was actually wrong:

- `control/ingestion`, declared `implemented`, imports `go.mindclade.dev/control/orchestration`
  from non-test production code (`control/ingestion/pipeline.go`, `control/ingestion/stage.go`).
  Orchestration is now an `implemented` component backed by substantive owning tests, so the
  edge satisfies the policy and its dated exception has been removed. The exception table is
  empty again, which is its intended steady state.

The rule binds every status that does not carry the flag — `implemented`, `qualified`,
`production` and `deprecated` — not only `production`, so it was never vacuous.

One adjacent edge stays outside the join and is recorded here rather than silently omitted:
`services/control_plane/tests/api_test.go` imports `control/routing`, which is `implemented`.
That is test code, not a linked production edge, and `services/control_plane` is still
undeclared, so it is not a production-track source the sweep can anchor on either way.

## Service-role declaration boundary

`services/studio` and `services/go_vanity` remain deployable-level `implemented` components.
The control plane now records all eleven separately shipped commands: registry is `qualified`,
API and maintenance are `implemented`, and the other eight roles remain `experimental`. Those
statuses reuse owning provider/foundation tests and do not imply every shared internal package
is qualified.

The remaining structural gap is explicit: `_GO_DECLARATION_GOVERNED_ROOTS` still governs only
`control`, `libs`, `services/go_vanity`, and `services/studio`. The command records live under
`services/control_plane/cmd/*`, while their shared implementation lives under `internal/`.
Before the entire `services` root can be claimed governed, a path-ownership rule must associate
each internal package with every shipping role that consumes it. The eight experimental roles
also require their named connected readiness evidence before advancement.

## What would change the answer

For any single component, in order: reference its existing `docs/slo/` and `docs/runbooks/`
documents from `components.toml` so both records agree; produce a qualification record that
states what passed rather than what remains pending; add a `ci/release/targets.yaml` entry and
merge a release request against it. The first step is bookkeeping. The second and third require
connected evidence that this repository does not contain.
