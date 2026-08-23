# Rust API Stability

- **stable** crates require additive changes or an approved compatibility epoch.
- **evolving** crates require migration tests and affected-consumer qualification.
- **compatibility** crates are temporary facades and may only lose APIs through a
  repository-wide migration.

Crate-to-tier assignment lives in `stability.json` and is gated against the crate
directories by `tools/analysis/check_rust_package_manifest.py`. The compatibility
tier is empty: the 2026-08 epoch crates were removed, not deprecated.

Persisted formats are versioned independently from Rust type layout. A breaking
format change requires dual read, migration tooling, rollback support, and a
release evidence bundle. `cargo public-api` baselines are generated in connected
CI; `public_api_baseline.json` is the offline snapshot of those symbols.

`public_api_baseline.json` is **partial**, and deliberately so: it records only
the crates whose symbols a connected `cargo public-api` run has actually
captured, because a baseline inferred from a `pub` grep would assert a surface
nobody verified. Roughly half the crates have no entry yet, so the file is not an
inventory and must not be read as one — `PACKAGE_MANIFEST.json` is. The checker
enforces the one direction that is sound offline: every crate the baseline names
must exist in the Cargo workspace. Closing the coverage gap requires a connected
CI run, not an edit to this file.
