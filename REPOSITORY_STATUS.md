# Repository implementation status

## Canonical status rule

A path may be materialized before it is production-ready. Source implementation,
offline qualification, connected qualification, and production promotion are
separate states. Machine-readable component state is recorded in
`components.toml` and `maturity.toml`; `VALIDATION.md` and `QUALIFICATION.md`
record evidence.

## Substantive implemented foundations

- complete layered `libs/go` mechanism foundation and durable coordination;
- standard Go process assembly through `servicekit/production`;
- typed Go control-plane domains under `control/`, including runtime authority,
  routing, artifacts, orchestration, reference releases, and release-evidence
  validation seams;
- Go architecture, dependency, root-module, and library-admission enforcement;
- runnable Go control-plane, event-dispatcher, and ingestion integrations;
- the user-supplied Rust foundation adopted as the literal starting point and
  deepened with cohesive runtime/node primitives, worker protocol/runtime,
  resource budgets, manifests/byte/storage foundations, and runtime
  gateway/host cores;
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
TileLang  qualified accelerator kernels
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
