# ADR-0019: Consolidate Rust around cohesive runtime mechanisms

- **Status:** Accepted
- **Date:** 2026-08-13
- **Supersedes:** the earlier shallow `common`/single-file scaffold organization, not the uploaded library semantics

## Decision

The uploaded 21-crate Rust foundation is the source baseline. Preserve its
bounded/deterministic implementations while moving new production consumers to
cohesive crates: `runtime_core`, `bytes_io`, `manifests`, `telemetry`,
`bounded_parse`, `bio_formats`, `worker_protocol`, `worker_runtime`, `gpu_host`
and `python_bridge`. Remove `common`. Do not fragment the runtime into dozens of
single-purpose crates.

Legacy crates remain for one compatibility epoch. New compatibility edges are
rejected by `check_rust_workspace.py`.

> **Epoch closed, 2026-08.** The decision above stands as recorded, but the
> compatibility window it opened is shut. The seven legacy crates — `clock`,
> `retry`, `resource_version`, `observability`, `artifact_manifest`, `byte_spec`,
> and `python_bindings` — were **removed outright**, not retained as facades:
> their directories, workspace members, and `Cargo.lock` entries are gone, so a
> stale import is a compile error rather than a deprecation warning. `BASELINE`
> in `tools/analysis/check_rust_workspace.py` is now empty, which means no
> compatibility edge is grandfathered any more. See
> `libs/rust/MIGRATION_2026_08.md`.

## Rationale

Rust must be as deep operationally as Go while remaining complementary. The
node/runtime layer benefits from stronger mechanisms, not more conceptual
surface area.
