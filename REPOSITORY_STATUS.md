# Mindclade · Repository implementation status

## Canonical status rule

A path may be materialized before it is production-ready. Source implementation,
offline qualification, connected qualification, and production promotion are
separate states. Machine-readable component state is recorded in
`components.toml` and `maturity.toml`; `VALIDATION.md` and `QUALIFICATION.md`
record evidence.

Every package under `control/` and `libs/` that contains non-scaffold production
Go now carries a `components.toml` entry, enforced by
`tools/analysis/check_component_maturity.py`. Before that gate existed a path
could hold production code and simply not appear in the record, and an
undeclared path inherits no status — so the model's central prohibition,
that production may not depend on `planned`/`scaffolded`/`experimental`, had
nothing to attach to. Deployable records now cover `services/studio`,
`services/go_vanity`, and all eleven `services/control_plane/cmd/*` roles. The
shared `services/control_plane/internal` packages are attributed through those
role tests but remain outside the governed-root invariant; closing that path
ownership gap is explicit maturity work. Directories whose Go is only
`const scaffold_<name>` placeholders stay undeclared by design.

## Current gate status (2026-08-23)

The complete static presubmit passes. `ci/presubmit/pipeline.py --static-only`
runs the 29 checkers in the `CHECKS` list of
`tools/analysis/run_architecture_checks.py`, and all 29 report `PASS` —
including the dependency-budget gate that previously blocked this lane.
Documentation, root policy, proprietary header, and dependency-license gates
also pass.

The `services/control_plane -> go.mindclade.dev/protocols/servicepolicy` import
is still present in `internal/providers/api`, but it is now inside the declared
budget: `tools/analysis/check_dependency_budgets.py` reports `dependency budget
check passed`. The reconciliation happened in source; this document previously
described the pre-reconciliation state and was wrong to call the lane blocked.

A passing static lane is repository-only evidence. It carries no connected
provider, GPU, deployment, or production-promotion evidence.

### Component census

`components.toml` declares 106 components:

```text
implemented   90
experimental   9   preprocessing.core and eight control-plane roles
scaffolded     4   models, training, evaluation, kernels
qualified      3   libs.go, control.model_registry, services.control_plane.registry
production     0
```

No component holds `production` status. Every readiness statement below is
bounded by that fact.

## Substantive implemented foundations

- complete layered `libs/go` mechanism foundation and durable coordination;
- standard Go process assembly through `servicekit/production`;
- typed Go control-plane domains under `control/`, including runtime authority,
  artifacts, reference releases, ingestion, release lineage, canonical evidence,
  append-only evidence-ledger storage, and signed production-eligibility decisions.
  `control/orchestration` and `control/scheduling` advanced to `implemented` on
  2026-08-23: the ten `control/orchestration` files that were `const scaffold_*`
  placeholders — service, executor, state machine, lease, compiler, repository,
  and the three launcher adapters — now carry implementations, and a shared
  `launchertest` conformance suite is run by all three launchers. `implemented`
  is not `qualified`; neither component has connected-provider or cluster
  evidence. `control/routing` is now `implemented`: signed canonical snapshots
  are isolated from caller mutation, failed publication retains an exact retryable
  snapshot, and package tests cover these invariants. A durable repository and
  connected publisher remain explicit qualification work;
- Go architecture, dependency, root-module, and library-admission enforcement;
- runnable Go control-plane, event-dispatcher, and ingestion integrations;
- the user-supplied Rust foundation adopted as the literal starting point and
  deepened with cohesive runtime/node primitives, worker protocol/runtime,
  resource budgets, the `manifests`, `bytes_io`, `object_store`, and `record_io`
  crates, and runtime gateway/host cores. (`byte_spec` and `artifact_manifest`
  are among the seven crates **removed** in the 2026-08 consolidation, not
  renamed facades — see `libs/rust/MIGRATION_2026_08.md`.)
- deterministic Python resolved-configuration implementation;
- scientific preprocessing contracts, durable DAG/cache/provenance boundaries,
  and model-independent MSA/template/ligand/feature stage structure;
- canonical runtime/artifact/reference/evidence protocols and cross-language
  fixtures;
- machine-readable component maturity and dependency-budget enforcement;
- reusable Google Cloud Terraform modules for hierarchy, keyless identity,
  security, private GKE/CPU/GPU compute, data/event/storage, build caches,
  audit, SLOs, and multi-project observability, with offline contract tests;
- comprehensive architecture, ADR, runbook, security, adoption, and
  qualification documentation.

## Implemented but not fully production-qualified

The Rust runtime foundation, runtime gateway/host cores, Python preprocessing
foundation, TypeScript SDK/library layer, web application cores, Terraform
module library, and several cross-language contracts contain substantive source but
still require the pinned Rust/build toolchain, provider integration,
performance, failure-injection, and/or cloud qualification described in
`QUALIFICATION.md`.

## Scaffolded or partial target-state areas

Many model families, full training/evaluation/serving implementations,
provider-specific scientific search adapters, live TypeScript environment integrations,
live environment Terraform roots/deployments, and scale-specific qualification paths remain
partial or scaffolded unless their own component status states otherwise.

Their presence communicates architecture and ownership, not release readiness.
A component advances only when it has an owner, stable contract, implementation,
meaningful tests, Bazel target, operational limits, security review where
applicable, and qualification evidence.

## Language law

```text
Go        fleet control plane and durable policy
Rust      online/runtime data plane and node execution
Python    scientific, model, training, inference, and evaluation numerics
TileLang  qualification-gated accelerator kernels (always behind a fallback)
TypeScript product surfaces and generated/public web clients
```

Go services consume `libs/go` mechanisms but keep reusable business policy under
`control/` and process composition under `services/`. Rust runtime services
consume `libs/rust` mechanisms but do not own model/tensor semantics. Python is
the final authority over scientific/numerical behavior. TileLang is always
behind qualification and fallback.

See `docs/architecture/system-design-reference.md` for the complete design.

## Final hardening state

The foundation architecture is frozen pending connected production qualification. Affected testing, artifact GC, Rust promotion/supply-chain/compatibility/failure/performance policies, node diagnostics/resource accounting, canonical workload envelopes, ownership metadata, and four golden vertical slices are implemented and locally validated. Future structural expansion requires measured workload evidence rather than symmetry.
