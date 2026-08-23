# Mindclade · Repository materialization status

## Status model

Materialized source and production readiness are deliberately separate. Use:

```text
planned -> scaffolded -> experimental -> implemented -> qualified -> production
```

`maturity.toml` declares seven statuses: the six above plus `deprecated`, which
is retirement rather than a rung on the ladder.

The authoritative machine-readable state is in `components.toml` and
`maturity.toml`; this page summarizes the important repository-wide boundaries.

## Census (2026-08-23)

`components.toml` declares 106 components:

```text
implemented   90
experimental   9   preprocessing.core and eight control-plane roles
scaffolded     4   models, training, evaluation, kernels
qualified      3   libs.go, control.model_registry, services.control_plane.registry
production     0
```

Nothing in this repository is `production`. Blueprint path materialization is
complete — 4,494 of 4,494 manifest paths, 100.0% — and that is a statement about
files existing, not about any of them being release-ready.

## Implemented substantive areas

The following contain real implementation/contracts/tests rather than merely
empty target-state placeholders:

- `libs/go/`: reusable Go mechanisms, durable coordination, production
  lifecycle, transports, storage/security/provider seams;
- `control/`: Go durable policy domains, including runtime authority, routing,
  artifacts, workflow/stage/attempt orchestration with its three launchers,
  scheduling admission/placement policy, registry/reference/evidence seams;
- `services/control_plane/internal/{bootstrap,foundation}/`: standardized role
  manifests, typed capabilities, and fail-closed composition;
- `libs/rust/`: the supplied Rust codebase as the starting implementation,
  deepened with consolidated runtime/node primitives, bounded resource/fencing
  contracts, storage/manifest/worker foundations, and related tests/contracts;
- Rust runtime gateway/host core paths: substantive local validation,
  admission/resource/supervision boundaries. The pinned Rust 1.97.1 toolchain now
  resolves in the Nix shell, and the canonical Cargo presubmit passed in the
  2026-08-20 run recorded in `VALIDATION.md`; network/provider qualification
  remains outstanding;
- deterministic Python configuration resolution and preprocessing
  contract/DAG/cache/provenance foundations;
- package-tested PyTorch attention, normalization, neural-network primitives,
  decoder-only LLM reference, and trusted-digest export leaves, without
  accelerator, serving, or production qualification claims;
- runtime/artifact/reference/evidence protocol contracts and cross-language
  fixtures;
- the public OpenAPI projection, generated TypeScript SDK and protobuf bindings,
  reusable TypeScript browser libraries, and the console and governance web
  application surfaces, pending live identity/API/security/deployment qualification;
- `tools/analysis/`: architecture, maturity, dependency, Go admission, and
  workspace consistency checks;
- `tools/qualification/go/`: offline and connected Go qualification lanes;
- `infra/terraform/modules/`: reusable Google Cloud module contracts spanning
  hierarchy, identity, networking, security, storage/data/event services,
  GKE/CPU/GPU compute, build caches, audit, SLOs, and metrics scopes;
- runnable Go examples for control-plane API, event dispatch, and ingestion;
- complete architecture/ADR/runbook/security/qualification documentation.

## Implemented but unqualified leaves

A substantive source implementation may still be blocked from `qualified` or
`production` by unavailable toolchains/providers or missing performance,
security, or failure evidence. In particular, Rust fuzz/Miri/sanitizers,
Tokio/Tonic network leaves, KMS-backed production signing, real cloud provider
integration (including Terraform connected plans/applies, drift, restore, and
environment roots), OS-specific optimized bulk IPC, and scale qualification remain
explicit promotion work where noted. Toolchain *availability* is no longer among
them — `rustc 1.97.1` resolves in the pinned Nix shell — but that is availability,
not a standing build claim: this document does not assert a green Cargo run at any
date other than the one `VALIDATION.md` records.

## Scaffolded/partial target-state areas

Many broader model, training, serving, evaluation, application, infrastructure,
and provider-specific scientific paths still reserve the production blueprint's
ownership/dependency boundaries without claiming full product implementation.

A component becomes production-ready only with an owner, stable contract,
implementation, meaningful tests, Bazel target, documentation, bounded
operational behavior, security review where relevant, qualification evidence,
and rollback path.

See `docs/architecture/system-design-reference.md`, `REPOSITORY_STATUS.md`,
`VALIDATION.md`, and `QUALIFICATION.md`.
