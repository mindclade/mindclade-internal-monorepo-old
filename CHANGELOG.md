# Changelog

All notable changes to the repository architecture and released implementation
surfaces are recorded here. Individual model, dataset, runtime, and service
releases also carry immutable release manifests and evidence bundles.


## 2026-08-13 — Eighteen optimizations and canonical system design

### Added

- the user-supplied Rust library as the authoritative `libs/rust` starting
  implementation, followed by cohesive runtime/node deepening rather than a
  rewrite;
- signed runtime authority, route/revocation snapshots, execution/admission
  tickets, unified durable stages, artifact identity, reference releases, and
  release-evidence graph seams;
- deterministic resolved configuration, component maturity, dependency budgets,
  Go library admission, root-module, and Rust workspace enforcement;
- substantive preprocessing DAG/cache/provenance foundations and cross-language
  protocol fixtures;
- `docs/architecture/system-design-reference.md` as the canonical end-to-end
  system design covering control, runtime, data, preprocessing, models, training,
  serving, evaluation, artifacts, release, security, failures, scheduling, and
  qualification;
- `docs/architecture/system-design-traceability.md` mapping design decisions to
  source paths, ADRs, and evidence.

### Reconciliation and reproducibility

- made `docs/architecture/system-design-reference.md` the executable design contract
  through a code/docs alignment presubmit check;
- promoted only the implemented Rust runtime gateway/host cores to `implemented`
  maturity while retaining explicit production-qualification blockers;
- converted seven legacy Rust crate names into deprecated compatibility facades and
  removed active production dependencies on those legacy names;
- restored all 4,475 blueprint paths after the Rust consolidation using explicit
  non-authoritative scaffold markers where the detailed target tree no longer maps
  one-to-one to canonical crates;
- populated root `go.sum` with both checksum records for all 18 direct public Go
  requirements and added connected `download/verify/tidy` gates for transitive closure;
- replaced the repository-validation scaffold with executable hygiene, structured-file,
  Markdown-link, architecture, and optional Go qualification checks.

### Documentation

- refreshed repository and scaffold status language to distinguish implemented
  source from connected/provider/performance qualification;
- expanded MkDocs navigation through ADR-0023 and all post-scaffold architecture
  chapters;
- added architecture-level invariants, outage semantics, boundedness rules,
  service decomposition triggers, and end-to-end pipeline sequence descriptions.

## 2026-08-13 — Production scaffold and Go foundation

### Added

- complete target-state monorepo scaffold materialized from the production
  blueprint;
- fully implemented layered `libs/go` foundation;
- durable outbox, inbox, cursor, projector, work queue, leadership, migration,
  configuration, signing, pagination, and resource-version mechanisms;
- standardized `servicekit/production` composition and role capability checks;
- Go control-plane domain packages and fail-closed command roots;
- runnable control-plane API, event-dispatcher, and ingestion-coordinator
  examples using local adapters;
- architecture chapters, accepted ADRs, security docs, runbooks, Go usage
  cookbook, qualification records, and scaffold status documentation.

### Architecture

- Go owns fleet control and durable policy;
- Rust owns online/runtime data-plane and node execution;
- Python/PyTorch owns scientific and numerical semantics;
- TileLang kernels are enabled only through qualification manifests;
- Bazel owns the build/release graph and Nix owns pinned toolchains.

### Qualification note

Offline Go qualification is recorded in `VALIDATION.md` and `QUALIFICATION.md`.
Connected provider, Bazel, Nix, Rust, Python numerical, TileLang, and deployment
qualification remain explicit promotion gates rather than implied claims.
